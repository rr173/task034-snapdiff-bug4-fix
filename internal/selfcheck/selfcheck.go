package selfcheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"

	"task034-snapdiff/internal/httpapi"
	"task034-snapdiff/internal/snapdiff"
)

// sha returns the SHA-256 hex digest of s, matching the service's hashing.
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type apiClient struct {
	base string
	c    *http.Client
	srv  *httptest.Server
}

// newClient starts a fresh server per check so that snapshot state never
// leaks between checks.
func newClient() *apiClient {
	srv := httptest.NewServer(httpapi.New().Handler())
	return &apiClient{base: srv.URL, c: srv.Client(), srv: srv}
}

func (a *apiClient) close() { a.srv.Close() }

func (a *apiClient) put(id string, files []snapdiff.FileInput) (int, string) {
	body, _ := json.Marshal(map[string]any{"files": files})
	req, _ := http.NewRequest(http.MethodPost, a.base+"/snapshots/"+id, bytes.NewReader(body))
	resp, err := a.c.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	var out struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Error
}

func (a *apiClient) diff(aID, bID string) (int, snapdiff.DiffReport) {
	resp, err := a.c.Get(a.base + "/diff?a=" + aID + "&b=" + bID)
	if err != nil {
		return 0, snapdiff.DiffReport{}
	}
	defer resp.Body.Close()
	var r snapdiff.DiffReport
	json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r
}

func eq(label string, got, want any) error {
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("%s mismatch:\n  got:  %#v\n  want: %#v", label, got, want)
	}
	return nil
}

func emptyReport() snapdiff.DiffReport {
	return snapdiff.DiffReport{
		Added:           []string{},
		Removed:         []string{},
		Modified:        []snapdiff.ModChange{},
		MetadataChanged: []snapdiff.MetaChange{},
		Renamed:         []snapdiff.Rename{},
		Unchanged:       0,
	}
}

// Run executes all self-checks against in-process servers. Returns 0 on
// success and 1 on the first failure.
func Run() int {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"healthz", checkHealthz},
		{"identical", checkIdentical},
		{"added_removed", checkAddedRemoved},
		{"modified", checkModified},
		{"metadata_changed", checkMetadataChanged},
		{"rename", checkRename},
		{"rename_guard", checkRenameGuard},
		{"rename_mixed", checkRenameMixed},
		{"dup_path_400", checkDupPath},
		{"missing_snapshot_404", checkMissing},
		{"empty_snapshots", checkEmpty},
	}
	for _, c := range checks {
		if err := c.fn(); err != nil {
			fmt.Printf("FAIL %s: %v\n", c.name, err)
			return 1
		}
		fmt.Printf("ok %s\n", c.name)
	}
	fmt.Println("OK")
	return 0
}

func checkHealthz() error {
	c := newClient()
	defer c.close()
	resp, err := c.c.Get(c.base + "/healthz")
	if err != nil {
		return fmt.Errorf("healthz: %v", err)
	}
	defer resp.Body.Close()
	if err := eq("healthz status", resp.StatusCode, http.StatusOK); err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return eq("healthz body", out.OK, true)
}

func checkIdentical() error {
	c := newClient()
	defer c.close()
	files := []snapdiff.FileInput{{Path: "f.txt", Content: "x", Mode: 1}}
	if code, _ := c.put("a", files); code != http.StatusOK {
		return fmt.Errorf("put a: %d", code)
	}
	if code, _ := c.put("b", files); code != http.StatusOK {
		return fmt.Errorf("put b: %d", code)
	}
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Unchanged = 1
	return eq("diff", got, want)
}

func checkAddedRemoved() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{
		{Path: "keep.txt", Content: "k", Mode: 1},
		{Path: "gone.txt", Content: "g", Mode: 1},
	})
	c.put("b", []snapdiff.FileInput{
		{Path: "keep.txt", Content: "k", Mode: 1},
		{Path: "new.txt", Content: "n", Mode: 1},
	})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Added = []string{"new.txt"}
	want.Removed = []string{"gone.txt"}
	want.Unchanged = 1
	return eq("diff", got, want)
}

func checkModified() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{{Path: "f.txt", Content: "old", Mode: 420}})
	c.put("b", []snapdiff.FileInput{{Path: "f.txt", Content: "new", Mode: 420}})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Modified = []snapdiff.ModChange{{
		Path:   "f.txt",
		Before: snapdiff.FileSummary{Size: 3, Hash: sha("old"), Mode: 420},
		After:  snapdiff.FileSummary{Size: 3, Hash: sha("new"), Mode: 420},
	}}
	return eq("diff", got, want)
}

func checkMetadataChanged() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{{Path: "f.txt", Content: "same", Mode: 420}})
	c.put("b", []snapdiff.FileInput{{Path: "f.txt", Content: "same", Mode: 644}})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.MetadataChanged = []snapdiff.MetaChange{{Path: "f.txt", Before: 420, After: 644}}
	return eq("diff", got, want)
}

func checkRename() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{{Path: "x.txt", Content: "hello", Mode: 420}})
	c.put("b", []snapdiff.FileInput{{Path: "y.txt", Content: "hello", Mode: 420}})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Renamed = []snapdiff.Rename{{From: "x.txt", To: "y.txt", Hash: sha("hello")}}
	return eq("diff", got, want)
}

func checkRenameGuard() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{
		{Path: "a.txt", Content: ""},
		{Path: "b.txt", Content: ""},
	})
	c.put("b", []snapdiff.FileInput{
		{Path: "c.txt", Content: ""},
		{Path: "d.txt", Content: ""},
	})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Added = []string{"c.txt", "d.txt"}
	want.Removed = []string{"a.txt", "b.txt"}
	return eq("diff", got, want)
}

func checkRenameMixed() error {
	c := newClient()
	defer c.close()
	c.put("a", []snapdiff.FileInput{
		{Path: "mv.txt", Content: "move"},
		{Path: "del.txt", Content: "del"},
		{Path: "keep.txt", Content: "keep", Mode: 5},
	})
	c.put("b", []snapdiff.FileInput{
		{Path: "moved.txt", Content: "move"},
		{Path: "keep.txt", Content: "keep", Mode: 7},
		{Path: "brand.txt", Content: "brand"},
	})
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	want := emptyReport()
	want.Added = []string{"brand.txt"}
	want.Removed = []string{"del.txt"}
	want.MetadataChanged = []snapdiff.MetaChange{{Path: "keep.txt", Before: 5, After: 7}}
	want.Renamed = []snapdiff.Rename{{From: "mv.txt", To: "moved.txt", Hash: sha("move")}}
	return eq("diff", got, want)
}

func checkDupPath() error {
	c := newClient()
	defer c.close()
	code, _ := c.put("a", []snapdiff.FileInput{
		{Path: "dup.txt", Content: "1"},
		{Path: "dup.txt", Content: "2"},
	})
	return eq("dup status", code, http.StatusBadRequest)
}

func checkMissing() error {
	c := newClient()
	defer c.close()
	code, _ := c.diff("nope-a", "nope-b")
	return eq("missing status", code, http.StatusNotFound)
}

func checkEmpty() error {
	c := newClient()
	defer c.close()
	if code, _ := c.put("a", []snapdiff.FileInput{}); code != http.StatusOK {
		return fmt.Errorf("put a: %d", code)
	}
	if code, _ := c.put("b", []snapdiff.FileInput{}); code != http.StatusOK {
		return fmt.Errorf("put b: %d", code)
	}
	code, got := c.diff("a", "b")
	if err := eq("status", code, http.StatusOK); err != nil {
		return err
	}
	return eq("diff", got, emptyReport())
}
