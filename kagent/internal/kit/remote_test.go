package kit

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type echoExecutor struct{}

func (echoExecutor) Execute(_ context.Context, rc *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if !yield(a2a.NewSubmittedTask(rc, rc.Message), nil) {
			return
		}
		reply := a2a.NewMessageForTask(a2a.MessageRoleAgent, rc, a2a.NewTextPart("billing answer"))
		yield(a2a.NewStatusUpdateEvent(rc, a2a.TaskStateCompleted, reply), nil)
	}
}

func (echoExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(func(a2a.Event, error) bool) {}
}

// urlRoute is the rule of kagent's gateway (httpAgentRoute in
// go/core/internal/a2agateway): the URL names the agent, and a tenant in the
// request has to agree with it.
type urlRoute struct {
	a2asrv.PassthroughCallInterceptor
	agent string
}

func (r *urlRoute) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if tenant := callCtx.Tenant(); tenant != "" && tenant != r.agent {
		return ctx, nil, a2a.NewError(a2a.ErrInvalidRequest, "tenant does not match the Agent URL")
	}
	return a2a.AttachTenant(ctx, r.agent), nil, nil
}

// fakeGateway serves one agent per URL, as kagent's gateway does.
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	handler := a2asrv.NewHandler(echoExecutor{})
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /agents/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
		agent := r.PathValue("namespace") + "/" + r.PathValue("name")
		a2asrv.NewJSONRPCHandler(&a2asrv.InterceptedHandler{
			Handler: handler, Interceptors: []a2asrv.CallInterceptor{&urlRoute{agent: agent}},
		}).ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /agents/{namespace}/{name}"+a2asrv.WellKnownAgentCardPath, func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("namespace") + "/" + r.PathValue("name")
		card := &a2a.AgentCard{Name: "billing_agent", Version: "v1",
			SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(srv.URL+"/agents/"+ref, a2a.TransportProtocolJSONRPC)},
			Capabilities:        a2a.AgentCapabilities{Streaming: true},
			DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A hand-off is made while this agent serves a call of its own, so the context
// carries this agent's tenant. The other agent must not get it.
func TestRemoteClientDoesNotPassItsOwnTenantOn(t *testing.T) {
	srv := fakeGateway(t)
	card := &a2a.AgentCard{Name: "billing_agent", SupportedInterfaces: []*a2a.AgentInterface{
		a2a.NewAgentInterface(srv.URL+"/agents/dp-ns/billing-agent", a2a.TransportProtocolJSONRPC)}}
	serving := a2a.AttachTenant(context.Background(), "dp-ns/support-triage")
	ask := func(factory *a2aclient.Factory) (a2a.SendMessageResult, error) {
		client, err := factory.CreateFromCard(serving, card)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = client.Destroy() }()
		return client.SendMessage(serving, &a2a.SendMessageRequest{
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Account ACC-1042 ...")),
		})
	}

	// The library's default sends the tenant along, and the gateway refuses.
	if _, err := ask(a2aclient.NewFactory()); err == nil || !strings.Contains(err.Error(), "tenant does not match the Agent URL") {
		t.Fatalf("a default client was expected to be refused over its tenant, got err = %v", err)
	}

	result, err := ask(remoteClientFactory())
	if err != nil {
		t.Fatalf("the hand-off was refused: %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok || task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("result = %#v", result)
	}
}
