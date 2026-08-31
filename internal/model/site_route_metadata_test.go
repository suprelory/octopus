package model

import "testing"

func TestSiteModelRouteMetadataRoundTrip(t *testing.T) {
	raw := SiteModelRouteMetadata{
		Source:                  "/api/pricing",
		RouteSupported:          true,
		RouteType:               SiteModelRouteTypeOpenAIResponse,
		EnableGroups:            []string{"vip", " default ", "vip"},
		SupportedEndpointTypes:  []string{"/v1/responses", "/v1/chat/completions", "/v1/responses"},
		HeuristicEndpointTypes:  []string{"/v1/responses", "/v1/responses"},
		NormalizedEndpointTypes: []string{string(SiteModelRouteTypeOpenAIResponse), string(SiteModelRouteTypeOpenAIChat)},
	}.Marshal()

	metadata, ok := ParseSiteModelRouteMetadata(raw)
	if !ok {
		t.Fatalf("expected marshaled metadata to parse")
	}
	if metadata.Kind != SiteModelRouteMetadataKind {
		t.Fatalf("expected kind %q, got %q", SiteModelRouteMetadataKind, metadata.Kind)
	}
	if metadata.RouteType != SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected route type %q, got %q", SiteModelRouteTypeOpenAIResponse, metadata.RouteType)
	}
	if len(metadata.SupportedEndpointTypes) != 2 {
		t.Fatalf("expected supported endpoint list to dedupe, got %#v", metadata.SupportedEndpointTypes)
	}
	if len(metadata.HeuristicEndpointTypes) != 1 || metadata.HeuristicEndpointTypes[0] != "/v1/responses" {
		t.Fatalf("expected heuristic endpoint list to dedupe, got %#v", metadata.HeuristicEndpointTypes)
	}
	if len(metadata.EnableGroups) != 2 || metadata.EnableGroups[0] != "default" || metadata.EnableGroups[1] != "vip" {
		t.Fatalf("expected enable groups to normalize and dedupe, got %#v", metadata.EnableGroups)
	}
}

func TestParseSiteModelRouteMetadataUnsupported(t *testing.T) {
	raw := SiteModelRouteMetadata{
		Source:                  "/api/pricing",
		RouteSupported:          false,
		SupportedEndpointTypes:  []string{"/vendor/embeddings"},
		NormalizedEndpointTypes: nil,
		UnsupportedReason:       "site reports endpoint types outside current supported route buckets",
	}.Marshal()

	metadata, ok := ParseSiteModelRouteMetadata(raw)
	if !ok {
		t.Fatalf("expected unsupported metadata to parse")
	}
	if metadata.RouteSupported {
		t.Fatalf("expected unsupported metadata to remain unsupported")
	}
	if metadata.RouteType != SiteModelRouteTypeUnknown {
		t.Fatalf("expected unsupported metadata route type %q, got %q", SiteModelRouteTypeUnknown, metadata.RouteType)
	}
}

func TestParseSiteModelRouteMetadataRetiresVolcengineRoute(t *testing.T) {
	raw := `{"kind":"site_route_metadata","version":1,"source":"/api/pricing","route_supported":true,"route_type":"volcengine","supported_endpoint_types":["ark"]}`
	metadata, ok := ParseSiteModelRouteMetadata(raw)
	if !ok {
		t.Fatal("expected legacy Volcengine metadata to remain readable")
	}
	if metadata.RouteSupported || metadata.RouteType != SiteModelRouteTypeUnknown {
		t.Fatalf("expected legacy route to become unsupported/unknown, got %+v", metadata)
	}
	if metadata.UnsupportedReason == "" {
		t.Fatal("expected legacy route removal reason to be retained")
	}
	if normalized := metadata.Marshal(); normalized == raw {
		t.Fatal("expected legacy metadata to be normalized on marshal")
	}
}

func TestParseSiteModelRouteMetadataRetainsRemovedEvidenceWhenUnsupported(t *testing.T) {
	raw := `{"kind":"site_route_metadata","version":1,"route_supported":false,"route_type":"volcengine","supported_endpoint_types":["https://ark.cn-beijing.volces.com/api/v3"]}`
	metadata, ok := ParseSiteModelRouteMetadata(raw)
	if !ok {
		t.Fatal("expected legacy unsupported metadata to remain readable")
	}
	if metadata.RouteSupported || metadata.RouteType != SiteModelRouteTypeUnknown {
		t.Fatalf("expected unsupported/unknown route, got %+v", metadata)
	}
	if metadata.UnsupportedReason == "" {
		t.Fatal("expected removed route reason to be synthesized")
	}
}

func TestContainsRemovedSiteModelRouteMarkerUsesTokenBoundaries(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "https://ark.cn-beijing.volces.com/api/v3", want: true},
		{value: "bytedance endpoint", want: true},
		{value: "notvolcengine/notdoubao", want: false},
		{value: "spark endpoint", want: false},
	}
	for _, tt := range tests {
		if got := ContainsRemovedSiteModelRouteMarker(tt.value); got != tt.want {
			t.Fatalf("ContainsRemovedSiteModelRouteMarker(%q) = %t, want %t", tt.value, got, tt.want)
		}
	}
}

func TestParseSiteModelRouteMetadataRejectsArbitraryPayload(t *testing.T) {
	if _, ok := ParseSiteModelRouteMetadata("mismatch"); ok {
		t.Fatalf("expected non-json payload to be rejected")
	}
	if _, ok := ParseSiteModelRouteMetadata(`{"kind":"other","version":1}`); ok {
		t.Fatalf("expected foreign metadata payload to be rejected")
	}
}
