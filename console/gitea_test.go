package main

import "testing"

func TestGiteaLink(t *testing.T) {
	g := newGiteaClient("http://gitea:3000/", "http://localhost:3001/", "geogrid", "support", "")
	cases := map[string]string{
		"http://gitea:3000/geogrid/support/issues/1":                "http://localhost:3001/geogrid/support/issues/1",
		"http://gitea:3000/geogrid/support/issues/1#issuecomment-7": "http://localhost:3001/geogrid/support/issues/1#issuecomment-7",
		"http://gitea:3000":                                 "http://localhost:3001",
		"http://gitea:30001/geogrid/support/issues/1":       "http://gitea:30001/geogrid/support/issues/1",
		"http://gitea.example.com/geogrid/support/issues/1": "http://gitea.example.com/geogrid/support/issues/1",
		"http://localhost:3001/geogrid/support/issues/2":    "http://localhost:3001/geogrid/support/issues/2",
	}
	for in, want := range cases {
		if got := g.link(in); got != want {
			t.Errorf("link(%q) = %q, want %q", in, got, want)
		}
	}

	same := newGiteaClient("http://localhost:3001", "", "geogrid", "support", "")
	if got := same.link("http://localhost:3001/geogrid/support/issues/1"); got != "http://localhost:3001/geogrid/support/issues/1" {
		t.Errorf("without a public URL, link changed the URL to %q", got)
	}
}
