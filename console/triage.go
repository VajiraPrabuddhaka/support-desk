package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

// triager runs triage jobs. A job sends one message to support-triage over A2A
// and records each step of its work as it streams back, so the page can show
// the steps while they happen. Jobs live in memory; the page polls them.
type triager struct {
	baseURL string

	mu       sync.Mutex
	jobs     map[string]*job
	byTicket map[int]*job // the latest job of each ticket
}

func newTriager(baseURL string) *triager {
	return &triager{
		baseURL:  strings.TrimRight(baseURL, "/"),
		jobs:     map[string]*job{},
		byTicket: map[int]*job{},
	}
}

// step is one thing the agents did, in words a person can follow.
type step struct {
	Seq    int       `json:"seq"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // tool | handoff | reply | say
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	Format string    `json:"format,omitempty"` // markdown | text | labels
	State  string    `json:"state"`            // running | done | failed
	Result string    `json:"result,omitempty"`
	callID string
}

// jobView is what the page sees of a job.
type jobView struct {
	ID        string     `json:"id"`
	Ticket    int        `json:"ticket"`
	Message   string     `json:"message"`
	State     string     `json:"state"` // running | done | failed
	Error     string     `json:"error,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Steps     []*step    `json:"steps"`
}

type job struct {
	mu sync.Mutex
	jobView
}

// start begins a triage of the ticket, or returns the one already running.
func (t *triager) start(ticket int, redo bool) *job {
	t.mu.Lock()
	defer t.mu.Unlock()
	if j := t.byTicket[ticket]; j != nil && j.snapshot().State == "running" {
		return j
	}
	message := fmt.Sprintf("Triage ticket #%d.", ticket)
	if redo {
		message = fmt.Sprintf("Redo the triage of ticket #%d.", ticket)
	}
	j := &job{jobView: jobView{ID: newID(), Ticket: ticket, Message: message, State: "running",
		StartedAt: time.Now(), Steps: []*step{}}}
	t.jobs[j.ID] = j
	t.byTicket[ticket] = j
	t.forgetOldJobs()
	go t.run(j)
	return j
}

func (t *triager) get(id string) *job {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.jobs[id]
}

func (t *triager) latest(ticket int) *job {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.byTicket[ticket]
}

func (t *triager) running(ticket int) bool {
	j := t.latest(ticket)
	return j != nil && j.snapshot().State == "running"
}

// forgetOldJobs keeps the 100 newest jobs. The caller holds t.mu.
func (t *triager) forgetOldJobs() {
	if len(t.jobs) <= 100 {
		return
	}
	all := make([]*job, 0, len(t.jobs))
	for _, j := range t.jobs {
		all = append(all, j)
	}
	sort.Slice(all, func(i, k int) bool { return all[i].StartedAt.After(all[k].StartedAt) })
	for _, j := range all[100:] {
		delete(t.jobs, j.ID)
		if t.byTicket[j.Ticket] == j {
			delete(t.byTicket, j.Ticket)
		}
	}
}

func (t *triager) run(j *job) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	slog.Info("triage started", "ticket", j.Ticket, "job", j.ID)

	card, err := agentcard.DefaultResolver.Resolve(ctx, t.baseURL)
	if err != nil {
		j.fail("support-triage is not reachable: " + err.Error())
		return
	}
	if err := pinToBase(card, t.baseURL); err != nil {
		j.fail(err.Error())
		return
	}
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		j.fail("cannot talk to support-triage: " + err.Error())
		return
	}
	defer func() { _ = client.Destroy() }()

	req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(j.Message))}
	for event, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			j.fail(friendlyError(err.Error()))
			return
		}
		switch e := event.(type) {
		case *a2a.TaskArtifactUpdateEvent:
			if e.Artifact != nil {
				j.addParts(e.Artifact.Parts)
			}
		case *a2a.Message:
			j.addParts(e.Parts)
		case *a2a.TaskStatusUpdateEvent:
			if e.Status.State == a2a.TaskStateFailed {
				j.fail(friendlyError(textOf(e.Status.Message)))
				return
			}
		case *a2a.Task:
			if e.Status.State == a2a.TaskStateFailed {
				j.fail(friendlyError(textOf(e.Status.Message)))
				return
			}
		}
	}
	j.finish()
	slog.Info("triage finished", "ticket", j.Ticket, "job", j.ID, "state", j.snapshot().State)
}

// pinToBase points the card's endpoints at the host the console was given. The
// card advertises the agent's in-cluster name, which may not resolve from here.
func pinToBase(card *a2a.AgentCard, baseURL string) error {
	base, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	for _, iface := range card.SupportedInterfaces {
		if iface == nil {
			continue
		}
		u, err := url.Parse(iface.URL)
		if err != nil {
			return fmt.Errorf("agent card has a bad URL %q: %w", iface.URL, err)
		}
		u.Scheme, u.Host = base.Scheme, base.Host
		iface.URL = u.String()
	}
	return nil
}

// addParts turns streamed parts into steps: tool calls, their results, and the
// model's own text.
func (j *job) addParts(parts a2a.ContentParts) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, p := range parts {
		if p == nil {
			continue
		}
		if text := p.Text(); text != "" {
			if isThought(p) || strings.TrimSpace(text) == "" {
				continue
			}
			j.Summary = strings.TrimSpace(text)
			j.addStep(&step{Kind: "say", Title: "support-triage", Detail: j.Summary, Format: "markdown", State: "done"})
			continue
		}
		data, _ := p.Data().(map[string]any)
		if data == nil {
			continue
		}
		name, _ := data["name"].(string)
		id, _ := data["id"].(string)
		switch partKind(p) {
		case "function_call":
			args, _ := data["args"].(map[string]any)
			s := describeCall(name, args)
			s.callID, s.State = id, "running"
			j.addStep(s)
		case "function_response":
			resp, _ := data["response"].(map[string]any)
			j.completeCall(id, name, resp)
		}
	}
}

// The labels that an agent's runtime puts on a part. ADK labels a tool call
// adk_type and a thought of the model adk_thought. kagent relabels both with
// its own keys, so the console reads either: the agents run on both.
var (
	partKindKeys = []string{"adk_type", "kagent.dev/a2a/part-type"}
	thoughtKeys  = []string{"adk_thought", "kagent.dev/a2a/thought"}
)

// partKind is function_call or function_response for a data part that carries
// one, and empty otherwise.
func partKind(p *a2a.Part) string {
	for _, key := range partKindKeys {
		if kind, _ := p.Metadata[key].(string); kind != "" {
			return kind
		}
	}
	return ""
}

// isThought is true for text that is the model's reasoning, not its answer.
func isThought(p *a2a.Part) bool {
	for _, key := range thoughtKeys {
		if thought, _ := p.Metadata[key].(bool); thought {
			return true
		}
	}
	return false
}

// completeCall marks the call done or failed, and adds billing-agent's answer
// as a step of its own.
func (j *job) completeCall(id, name string, resp map[string]any) {
	var call *step
	for i := len(j.Steps) - 1; i >= 0; i-- {
		if s := j.Steps[i]; s.callID != "" && s.callID == id {
			call = s
			break
		}
	}
	if call == nil {
		call = describeCall(name, nil)
		j.addStep(call)
	}
	if errVal, ok := resp["error"]; ok && errVal != nil && fmt.Sprint(errVal) != "" {
		call.State, call.Result = "failed", truncate(fmt.Sprint(errVal), 600)
		return
	}
	call.State = "done"
	if name == "billing_agent" {
		answer, _ := resp["result"].(string)
		if strings.TrimSpace(answer) == "" {
			answer = "(no answer)"
		}
		j.addStep(&step{Kind: "reply", Title: "billing-agent answered", Detail: strings.TrimSpace(answer), Format: "markdown", State: "done"})
	}
}

func (j *job) addStep(s *step) {
	s.Seq = len(j.Steps) + 1
	s.At = time.Now()
	j.Steps = append(j.Steps, s)
}

func (j *job) fail(msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	j.State, j.Error, j.EndedAt = "failed", msg, &now
	for _, s := range j.Steps {
		if s.State == "running" {
			s.State = "failed"
		}
	}
	slog.Warn("triage failed", "ticket", j.Ticket, "job", j.ID, "error", msg)
}

func (j *job) finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State != "running" {
		return
	}
	now := time.Now()
	j.State, j.EndedAt = "done", &now
	for _, s := range j.Steps {
		if s.State == "running" {
			s.State = "done"
		}
	}
}

// snapshot returns a copy that is safe to encode while the job runs.
func (j *job) snapshot() jobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	c := j.jobView
	c.Steps = make([]*step, len(j.Steps))
	for i, s := range j.Steps {
		cp := *s
		c.Steps[i] = &cp
	}
	return c
}

// describeCall puts a tool call in words.
func describeCall(name string, args map[string]any) *step {
	number := ""
	if v, ok := args["issue_number"].(float64); ok {
		number = fmt.Sprintf("#%d", int(v))
	}
	method, _ := args["method"].(string)
	s := &step{Kind: "tool"}
	switch name {
	case "issue_read":
		switch method {
		case "get":
			s.Title = "Read ticket " + number
		case "get_comments":
			s.Title = "Read the comments on " + number
		case "get_labels":
			s.Title = "Read the labels of " + number
		default:
			s.Title = strings.TrimSpace(fmt.Sprintf("Read %s (%s)", number, method))
		}
	case "list_issues":
		s.Title = "Looked through the ticket queue"
	case "search_issues":
		s.Title = "Searched the tickets"
	case "label_read":
		s.Title = "Looked up the labels"
	case "issue_write":
		switch method {
		case "add_labels":
			s.Title, s.Format = "Labelled "+number, "labels"
			var names []string
			if list, ok := args["labels"].([]any); ok {
				for _, l := range list {
					names = append(names, fmt.Sprint(l))
				}
			}
			s.Detail = strings.Join(names, ",")
		case "add_comment":
			s.Title, s.Format = "Wrote the triage note on "+number, "markdown"
			s.Detail, _ = args["body"].(string)
		default:
			s.Title = strings.TrimSpace(fmt.Sprintf("Updated %s (%s)", number, method))
		}
	case "billing_agent":
		s.Kind, s.Title, s.Format = "handoff", "Asked billing-agent over A2A", "markdown"
		s.Detail, _ = args["request"].(string)
	case "":
		s.Title = "Called a tool"
	default:
		s.Title = "Called " + name
		if len(args) > 0 {
			b, _ := json.Marshal(args)
			s.Detail, s.Format = truncate(string(b), 400), "text"
		}
	}
	return s
}

// friendlyError adds what usually helps when the model provider refuses a call.
func friendlyError(msg string) string {
	msg = strings.TrimSpace(msg)
	switch {
	case msg == "":
		return "support-triage stopped without saying why."
	case strings.Contains(msg, "429"):
		return msg + " (the model provider is rate-limiting requests; OpenRouter's free models allow 50 a day without credits)"
	case strings.Contains(msg, "402"):
		return msg + " (the OpenRouter account needs more credit)"
	}
	return msg
}

func textOf(m *a2a.Message) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p != nil {
			b.WriteString(p.Text())
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
