package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

const imagesUpstreamErrorBodyLimit = 16 * 1024

func imagesAttempt(
	ctx context.Context,
	endpoint string,
	c *gin.Context,
	bc *bodycache.BodyCache,
	isMultipart bool,
	boundary string,
	jsonPayload map[string]any,
	stream bool,
	channel *model.Channel,
	channelKey string,
	firstTokenTimeOutSec int,
	metrics *imagesRelayMetrics,
	actualModel string,
	hb *earlyHeartbeat,
	retryAtOut *time.Time,
) (statusCode int, written bool, usage *imagesUsage, upstreamCT string, err error) {
	// 构建 URL（baseUrl.Path 后追加 endpoint）
	baseURL := channel.GetBaseUrl()
	parsedURL, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to parse base url: %w", err))
	}
	parsedURL.Path = parsedURL.Path + endpoint

	var bodyReader io.Reader
	var contentType string

	if isMultipart {
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		contentType = mw.FormDataContentType()
		bodyReader = pr

		go func() {
			src, err := bc.NewReader()
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			defer src.Close()

			if err := copyMultipartReplaceModel(src, boundary, mw, actualModel); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			// 先关闭 multipart.Writer 写入结束 boundary，再关闭 pipe writer
			if err := mw.Close(); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			_ = pw.Close()
		}()
	} else {
		// JSON：仅改写 model 字段，其余保持不变
		// 注意：每次尝试都重新 marshal 生成 body，确保可重试重建
		if jsonPayload == nil {
			return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, errors.New("nil json payload"))
		}
		jsonPayload["model"] = actualModel
		b, err := json.Marshal(jsonPayload)
		if err != nil {
			return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to marshal json: %w", err))
		}
		if channelParamOverrideConfigured(channel) {
			b, _, err = helper.ApplyParamOverridePayload(b, channel.ParamOverride)
			if err != nil {
				return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, fmt.Errorf("invalid channel param override: %w", err))
			}
		}
		actualModel, err = requiredJSONModel(b)
		if err != nil {
			return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override produced an invalid images request: %w", err))
		}
		bodyReader = bytes.NewReader(b)
		contentType = "application/json"
	}
	if metrics != nil {
		metrics.ActualModel = actualModel
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bodyReader)
	if err != nil {
		return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to create request: %w", err))
	}
	req.URL = parsedURL
	req.Method = http.MethodPost

	// Header 透传：复制下游 header，过滤 hop-by-hop 与鉴权相关
	copyHeadersToUpstream(req, c, channel, channelKey, contentType, stream)

	// 发送请求
	httpClient, err := helper.ChannelHTTPClientWithContext(ctx, channel)
	if err != nil {
		return 0, false, nil, "", classifyLocalRelayError(FailureConfiguration, err)
	}

	respUp, err := httpClient.Do(req)
	if err != nil {
		return 0, false, nil, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer respUp.Body.Close()

	upstreamCT = respUp.Header.Get("Content-Type")

	// stream=true：逐行解析 event/data/空行边界透传
	if stream {
		if respUp.StatusCode < 200 || respUp.StatusCode >= 300 {
			if retryAtOut != nil {
				*retryAtOut = parseRetryAt(respUp.Header.Get("Retry-After"))
			}
			b, _ := io.ReadAll(io.LimitReader(respUp.Body, imagesUpstreamErrorBodyLimit))
			return respUp.StatusCode, false, nil, upstreamCT, transformerModel.NormalizeHTTPError(respUp.StatusCode, respUp.Header, b, "api_error")
		}
		u, w, err := proxySSE(ctx, c, respUp, firstTokenTimeOutSec, metrics, hb)
		return respUp.StatusCode, w, u, upstreamCT, err
	}

	// 非流式：2xx 透传，否则读取限长错误体用于错误信息与重试判定
	if respUp.StatusCode < 200 || respUp.StatusCode >= 300 {
		if retryAtOut != nil {
			*retryAtOut = parseRetryAt(respUp.Header.Get("Retry-After"))
		}
		b, _ := io.ReadAll(io.LimitReader(respUp.Body, imagesUpstreamErrorBodyLimit))
		return respUp.StatusCode, false, nil, upstreamCT, transformerModel.NormalizeHTTPError(respUp.StatusCode, respUp.Header, b, "api_error")
	}

	u, w, err := proxyNonStream(c, respUp)

	// Empty response detection: if nothing was written, treat as empty response
	if err == nil && !w {
		err = fmt.Errorf("empty image response: no data written")
	}

	return respUp.StatusCode, w, u, upstreamCT, err
}

// proxyNonStream 将上游非流式响应原样透传到下游，同时尽量提取 usage（避免解析巨大 b64_json）。
func proxyNonStream(c *gin.Context, respUp *http.Response) (*imagesUsage, bool, error) {
	ct := respUp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	c.Header("Content-Type", ct)
	c.Status(respUp.StatusCode)

	scanner := newUsageScanner()

	buf := make([]byte, 32*1024)
	for {
		n, rerr := respUp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			scanner.Feed(chunk)
			if _, werr := c.Writer.Write(chunk); werr != nil {
				return scanner.Usage(), true, werr
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return scanner.Usage(), c.Writer.Written(), rerr
		}
	}

	return scanner.Usage(), c.Writer.Written(), nil
}
