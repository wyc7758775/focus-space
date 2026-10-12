package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReaderCacheNamespaceIsolation(t *testing.T) {
	a := readerCacheNamespace("alice", "one")
	if len(a) != 64 || a != readerCacheNamespace("alice", "one") || a == readerCacheNamespace("bob", "one") || a == readerCacheNamespace("alice", "two") {
		t.Fatal("cache scope isolation failed")
	}
}
func TestReaderCacheNamespaceCapabilityFlow(t *testing.T) {
	r := callJSON(t, capabilityHandler, http.MethodGet, "/api/capabilities", "")
	var v map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || v["cache_namespace"] != readerCacheNamespace(appDataScope.OwnerID, appDataScope.ProfileID) {
		t.Fatal(v)
	}
}
