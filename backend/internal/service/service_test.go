package service

import (
	"testing"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/remna"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/repository"
)

func TestMapRemnaNodesNormalizesThreeLetterCountryCode(t *testing.T) {
	nodes := mapRemnaNodes([]remna.Node{
		{
			Name:         "FIN - OUT",
			Address:      "45.66.163.83",
			CountryCode:  "FIN",
			IsConnected:  true,
			ViewPosition: 1,
		},
	}, RemnaNodeSyncConfig{RequireTag: "OUT", ProxyPort: 1443})

	if len(nodes) != 1 {
		t.Fatalf("expected one mapped node, got %d", len(nodes))
	}
	if nodes[0].Country != "FI" {
		t.Fatalf("expected normalized country FI, got %q", nodes[0].Country)
	}
	if nodes[0].ID != "fi-1" {
		t.Fatalf("expected normalized node id fi-1, got %q", nodes[0].ID)
	}
}

func TestApplyRegisteredProxyCandidatesFallsBackByCountry(t *testing.T) {
	nodes := []repository.Node{
		{ID: "fi-3", Country: "FI", Host: "45.66.163.83", ProxyPort: 1443},
		{ID: "de-4", Country: "DE", Host: "45.144.178.107", ProxyPort: 1443},
	}
	candidates := []repository.Node{
		{ID: "de-1", Country: "DE", Host: "de2.clearforfun.tech", ProxyPort: 1443, ProxyScheme: "https"},
		{ID: "fi-1", Country: "FI", Host: "fin2.clearforfun.tech", ProxyPort: 1443, ProxyScheme: "https"},
	}

	applyRegisteredProxyCandidates(nodes, candidates)

	if nodes[0].Host != "fin2.clearforfun.tech" {
		t.Fatalf("expected Finland fallback endpoint, got %q", nodes[0].Host)
	}
	if nodes[1].Host != "de2.clearforfun.tech" {
		t.Fatalf("expected Germany fallback endpoint, got %q", nodes[1].Host)
	}
}

func TestApplyRegisteredProxyCandidatesPrefersExactID(t *testing.T) {
	nodes := []repository.Node{
		{ID: "de-1", Country: "DE", Host: "remna-one.example", ProxyPort: 1443},
		{ID: "de-2", Country: "DE", Host: "remna-two.example", ProxyPort: 1443},
	}
	candidates := []repository.Node{
		{ID: "de-2", Country: "DE", Host: "de2.clearforfun.tech", ProxyPort: 1443},
		{ID: "de-1", Country: "DE", Host: "de1.clearforfun.tech", ProxyPort: 1443},
	}

	applyRegisteredProxyCandidates(nodes, candidates)

	if nodes[0].Host != "de1.clearforfun.tech" {
		t.Fatalf("expected exact de-1 endpoint, got %q", nodes[0].Host)
	}
	if nodes[1].Host != "de2.clearforfun.tech" {
		t.Fatalf("expected exact de-2 endpoint, got %q", nodes[1].Host)
	}
}

func TestApplyRegisteredProxyCandidatesDoesNotReuseEndpoint(t *testing.T) {
	nodes := []repository.Node{
		{ID: "fi-2", Country: "FI", Host: "remna-one.example", ProxyPort: 1443},
		{ID: "fi-3", Country: "FI", Host: "remna-two.example", ProxyPort: 1443},
	}
	candidates := []repository.Node{
		{ID: "fi-1", Country: "FI", Host: "fin2.clearforfun.tech", ProxyPort: 1443},
		{ID: "legacy-fi", Country: "FI", Host: "fin2.clearforfun.tech", ProxyPort: 1443},
	}

	applyRegisteredProxyCandidates(nodes, candidates)

	if nodes[0].Host != "fin2.clearforfun.tech" {
		t.Fatalf("expected first node to receive the endpoint, got %q", nodes[0].Host)
	}
	if nodes[1].Host != "remna-two.example" {
		t.Fatalf("expected endpoint not to be reused, got %q", nodes[1].Host)
	}
}
