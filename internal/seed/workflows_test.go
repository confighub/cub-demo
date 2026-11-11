package seed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubclient"
)

// fakeComponentServer answers Component patches as a server that knows only field does, refusing
// a patch naming the other policy field the way ConfigHub refuses an unknown attribute. It records
// each patch it was sent.
func fakeComponentServer(t *testing.T, field string) (*Seeder, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var patches []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var patch map[string]any
		if err := json.Unmarshal(body, &patch); err != nil {
			t.Errorf("patch is not JSON: %s", body)
		}
		mu.Lock()
		patches = append(patches, patch)
		mu.Unlock()
		for _, other := range []string{"RequireChangeWorkflow", "ChangeWorkflowRequired"} {
			if _, named := patch[other]; named && other != field {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"patch contains invalid attributes","Error":{"Message":"patch contains invalid attributes","Type":"UserInput","Status":400,"Details":["` + other + ` is not a valid attribute of entity type Component"]}}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CUB_SERVER", server.URL)
	t.Setenv("CUB_TOKEN", "token")
	client, err := cubclient.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return &Seeder{Client: client}, &patches
}

// The policy is patched with the field's current name, and with the name it had before ConfigHub
// renamed it when the server is from before the rename. The rest of the patch goes both times.
func TestPatchWorkflowPolicyNamesTheFieldTheServerHas(t *testing.T) {
	for _, tc := range []struct {
		server string
		sent   []string
	}{
		{server: "RequireChangeWorkflow", sent: []string{"RequireChangeWorkflow"}},
		{server: "ChangeWorkflowRequired", sent: []string{"RequireChangeWorkflow", "ChangeWorkflowRequired"}},
	} {
		t.Run(tc.server, func(t *testing.T) {
			s, patches := fakeComponentServer(t, tc.server)
			err := s.patchWorkflowPolicy(uuid.New(), true, map[string]any{"Annotations": map[string]any{"a": "b"}})
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if len(*patches) != len(tc.sent) {
				t.Fatalf("sent %d patches, want %d: %v", len(*patches), len(tc.sent), *patches)
			}
			for i, field := range tc.sent {
				patch := (*patches)[i]
				if patch[field] != true {
					t.Errorf("patch %d: %s = %v, want true: %v", i, field, patch[field], patch)
				}
				if len(patch) != 2 || patch["Annotations"] == nil {
					t.Errorf("patch %d should carry the policy field and Annotations only: %v", i, patch)
				}
			}
		})
	}
}

// An error that is not about the field is returned as it is, without a second patch.
func TestPatchWorkflowPolicyReturnsOtherErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Manage permission required","Error":{"Message":"Manage permission required","Status":403}}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CUB_SERVER", server.URL)
	t.Setenv("CUB_TOKEN", "token")
	client, err := cubclient.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := &Seeder{Client: client}
	if err := s.patchWorkflowPolicy(uuid.New(), true, map[string]any{}); err == nil {
		t.Fatal("a refused patch should fail")
	}
}
