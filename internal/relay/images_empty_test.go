package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProxyNonStream_EmptyResponse(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantWritten bool
		wantErr     bool
	}{
		{
			name:        "valid response with data",
			body:        `{"data":[{"url":"https://example.com/image.png"}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`,
			wantWritten: true,
			wantErr:     false,
		},
		{
			name:        "empty response",
			body:        "",
			wantWritten: false,
			wantErr:     false, // proxyNonStream doesn't error on empty, just returns written=false
		},
		{
			name:        "response with only whitespace",
			body:        "   \n\t  ",
			wantWritten: true, // whitespace is still data
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			respUp := &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}

			_, written, err := proxyNonStream(c, respUp)

			if (err != nil) != tt.wantErr {
				t.Errorf("proxyNonStream() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if written != tt.wantWritten {
				t.Errorf("proxyNonStream() written = %v, want %v", written, tt.wantWritten)
			}

			// Verify response was actually written to the context
			if written && w.Body.Len() == 0 {
				t.Error("proxyNonStream() reported written=true but response body is empty")
			}
		})
	}
}

func TestUsageScanner_EmptyInput(t *testing.T) {
	scanner := newUsageScanner()

	// Feed empty data
	scanner.Feed([]byte{})

	usage := scanner.Usage()
	if usage != nil {
		t.Errorf("usageScanner with empty input should return nil usage, got %+v", usage)
	}
}

func TestUsageScanner_NoUsageField(t *testing.T) {
	scanner := newUsageScanner()

	// Feed JSON without usage field
	data := []byte(`{"data":[{"url":"https://example.com/image.png"}]}`)
	scanner.Feed(data)

	usage := scanner.Usage()
	if usage != nil {
		t.Errorf("usageScanner with no usage field should return nil, got %+v", usage)
	}
}

func TestUsageScanner_ValidUsage(t *testing.T) {
	scanner := newUsageScanner()

	// Feed JSON with usage field
	data := []byte(`{"data":[],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`)
	scanner.Feed(data)

	usage := scanner.Usage()
	if usage == nil {
		t.Fatal("usageScanner should extract usage, got nil")
	}

	if usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", usage.OutputTokens)
	}
	if usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", usage.TotalTokens)
	}
}
