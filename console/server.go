package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"support-desk/internal/tickets"
)

const triageBot = "triage-bot"

type server struct {
	gitea      *giteaClient
	lago       *lagoClient
	triage     *triager
	lagoUIURL  string
	web        fs.FS
	repoLabel  string
	agentCards *cardCache
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/tickets", s.listTickets)
	mux.HandleFunc("POST /api/tickets", s.createTicket)
	mux.HandleFunc("GET /api/tickets/{number}", s.getTicket)
	mux.HandleFunc("POST /api/tickets/{number}/triage", s.startTriage)
	mux.HandleFunc("GET /api/tickets/{number}/triage", s.latestTriage)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /api/accounts/{id}", s.getAccount)
	mux.Handle("GET /", noCache(http.FileServerFS(s.web)))
	return mux
}

// ticketSummary is a ticket as the queue shows it.
type ticketSummary struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Company   string    `json:"company"`
	AccountID string    `json:"accountId"`
	FromName  string    `json:"fromName"`
	FromEmail string    `json:"fromEmail"`
	Channel   string    `json:"channel"`
	Status    string    `json:"status"` // new | triaged | needs-info
	Type      string    `json:"type"`
	Priority  string    `json:"priority"`
	Labels    []label   `json:"labels"`
	Comments  int       `json:"comments"`
	Open      bool      `json:"open"`
	CreatedAt time.Time `json:"createdAt"`
	Triaging  bool      `json:"triaging"`
}

type label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ticketDetail is one ticket with the customer's message and its notes.
type ticketDetail struct {
	ticketSummary
	Message string `json:"message"`
	URL     string `json:"url"`
	Notes   []note `json:"notes"`
}

type note struct {
	Author    string    `json:"author"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	ByTriage  bool      `json:"byTriage"`
}

func (s *server) summarize(i giteaIssue) (ticketSummary, tickets.Ticket) {
	t := tickets.Parse(i.Title, i.Body)
	sum := ticketSummary{
		Number: i.Number, Title: i.Title, Company: t.Company, AccountID: t.AccountID,
		FromName: t.FromName, FromEmail: t.FromEmail, Channel: t.Channel,
		Status: "new", Comments: i.Comments, Open: i.State == "open", CreatedAt: i.CreatedAt,
		Labels: []label{}, Triaging: s.triage.running(i.Number),
	}
	for _, l := range i.Labels {
		sum.Labels = append(sum.Labels, label{Name: l.Name, Color: "#" + strings.TrimPrefix(l.Color, "#")})
		switch {
		case l.Name == "status/triaged":
			sum.Status = "triaged"
		case l.Name == "status/needs-info":
			sum.Status = "needs-info"
		case strings.HasPrefix(l.Name, "type/"):
			sum.Type = strings.TrimPrefix(l.Name, "type/")
		case strings.HasPrefix(l.Name, "priority/"):
			sum.Priority = strings.TrimPrefix(l.Name, "priority/")
		}
	}
	return sum, t
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	card := s.agentCards.get(r.Context(), s.triage.baseURL)
	writeJSON(w, http.StatusOK, map[string]any{
		"repo":      s.repoLabel,
		"agent":     card,
		"lagoUI":    s.lagoUIURL,
		"hasLago":   s.lago != nil,
		"triageBot": triageBot,
	})
}

func (s *server) listTickets(w http.ResponseWriter, r *http.Request) {
	issues, err := s.gitea.listIssues(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]ticketSummary, 0, len(issues))
	for _, i := range issues {
		sum, _ := s.summarize(i)
		out = append(out, sum)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getTicket(w http.ResponseWriter, r *http.Request) {
	number, ok := ticketNumber(w, r)
	if !ok {
		return
	}
	issue, err := s.gitea.getIssue(r.Context(), number)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	comments, err := s.gitea.listComments(r.Context(), number)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	sum, t := s.summarize(issue)
	d := ticketDetail{ticketSummary: sum, Message: t.Message, URL: s.gitea.link(issue.HTMLURL), Notes: []note{}}
	for _, c := range comments {
		d.Notes = append(d.Notes, note{
			Author: c.User.Login, Name: c.User.FullName, Body: c.Body, URL: s.gitea.link(c.HTMLURL),
			CreatedAt: c.CreatedAt, ByTriage: c.User.Login == triageBot,
		})
	}
	writeJSON(w, http.StatusOK, d)
}

type newTicketRequest struct {
	Company   string `json:"company"`
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
}

// createTicket files a new request as the support inbox, the way a request
// from the web form arrives.
func (s *server) createTicket(w http.ResponseWriter, r *http.Request) {
	var req newTicketRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("the request is not valid JSON"))
		return
	}
	for _, f := range []*string{&req.Company, &req.AccountID, &req.Name, &req.Email, &req.Subject, &req.Message} {
		*f = strings.TrimSpace(*f)
	}
	switch {
	case req.Subject == "" || req.Message == "":
		writeError(w, http.StatusBadRequest, errors.New("a subject and a message are needed"))
		return
	case req.Email == "":
		writeError(w, http.StatusBadRequest, errors.New("an email address is needed"))
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("the email address is not valid"))
		return
	}
	t := tickets.Ticket{
		Title: req.Subject, FromName: req.Name, FromEmail: req.Email, Company: req.Company,
		AccountID: req.AccountID, Channel: "web form", Message: req.Message,
	}
	issue, err := s.gitea.createIssue(r.Context(), t.Title, t.Body())
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	slog.Info("ticket filed", "number", issue.Number, "account", t.AccountID)
	sum, _ := s.summarize(issue)
	writeJSON(w, http.StatusCreated, sum)
}

func (s *server) startTriage(w http.ResponseWriter, r *http.Request) {
	number, ok := ticketNumber(w, r)
	if !ok {
		return
	}
	var req struct {
		Redo bool `json:"redo"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req)
	j := s.triage.start(number, req.Redo)
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

func (s *server) latestTriage(w http.ResponseWriter, r *http.Request) {
	number, ok := ticketNumber(w, r)
	if !ok {
		return
	}
	j := s.triage.latest(number)
	if j == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	j := s.triage.get(r.PathValue("id"))
	if j == nil {
		writeError(w, http.StatusNotFound, errors.New("no such triage run"))
		return
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}

func (s *server) getAccount(w http.ResponseWriter, r *http.Request) {
	if s.lago == nil {
		writeError(w, http.StatusNotFound, errors.New("the console has no Lago API key"))
		return
	}
	a, err := s.lago.account(r.Context(), r.PathValue("id"))
	if errors.Is(err, errAccountNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if s.lagoUIURL != "" && a.LagoID != "" {
		a.LagoURL = s.lagoUIURL + "/customer/" + a.LagoID
	}
	writeJSON(w, http.StatusOK, a)
}

func ticketNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("not a ticket number"))
		return 0, false
	}
	return n, true
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var ue *upstreamError
	if errors.As(err, &ue) && ue.Status == http.StatusNotFound {
		writeError(w, http.StatusNotFound, errors.New("no such ticket"))
		return
	}
	writeError(w, http.StatusBadGateway, err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

// agentStatus is what the page shows about support-triage.
type agentStatus struct {
	Online      bool   `json:"online"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// cardCache fetches support-triage's agent card at most every 15 seconds, to
// show whether the agent is up.
type cardCache struct {
	mu      sync.Mutex
	at      time.Time
	current agentStatus
}

func newCardCache() *cardCache { return &cardCache{} }

func (c *cardCache) get(ctx context.Context, baseURL string) agentStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < 15*time.Second {
		return c.current
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
	if err != nil {
		c.current = agentStatus{Online: false, Name: "support-triage"}
	} else {
		c.current = agentStatus{Online: true, Name: card.Name, Description: card.Description}
	}
	c.at = time.Now()
	return c.current
}
