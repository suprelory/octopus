package outbound

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type ConversionQuality string

const (
	QualityNative      ConversionQuality = "native"
	QualityLossless    ConversionQuality = "lossless"
	QualityConditional ConversionQuality = "conditional"
	QualityUnsupported ConversionQuality = "unsupported"
)

type QualityMatrixEntry struct {
	InboundFormat  model.APIFormat
	OutboundType   OutboundType
	OutboundFormat model.APIFormat
	RequestType    model.RequestType
	Quality        ConversionQuality
}

var matrixInboundFormats = []model.APIFormat{
	model.APIFormatOpenAIChatCompletion,
	model.APIFormatOpenAIResponse,
	model.APIFormatAnthropicMessage,
	model.APIFormatGeminiContents,
	model.APIFormatOpenAIEmbedding,
	model.APIFormatOpenAIImageGeneration,
}

var matrixRequestTypes = []model.RequestType{
	model.RequestTypeChat,
	model.RequestTypeResponses,
	model.RequestTypeEmbedding,
	model.RequestTypeImages,
	model.RequestTypeRerank,
}

// StaticQualityMatrix returns the complete protocol/operation matrix. Native
// means byte-stable passthrough is available, lossless means the canonical
// shape is fully represented, and conditional means request features decide
// whether the concrete conversion is degraded.
func StaticQualityMatrix() []QualityMatrixEntry {
	types := make([]OutboundType, 0, len(protocolDescriptors))
	for typ := range protocolDescriptors {
		types = append(types, typ)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	entries := make([]QualityMatrixEntry, 0, len(matrixInboundFormats)*len(types)*len(matrixRequestTypes))
	for _, inboundFormat := range matrixInboundFormats {
		for _, outboundType := range types {
			capability := protocolDescriptors[outboundType]
			for _, requestType := range matrixRequestTypes {
				entries = append(entries, QualityMatrixEntry{
					InboundFormat:  inboundFormat,
					OutboundType:   outboundType,
					OutboundFormat: capability.APIFormat,
					RequestType:    requestType,
					Quality:        StaticConversionQuality(inboundFormat, outboundType, requestType),
				})
			}
		}
	}
	return entries
}

func StaticConversionQuality(inboundFormat model.APIFormat, outboundType OutboundType, requestType model.RequestType) ConversionQuality {
	capability, ok := Descriptor(outboundType)
	if !ok || !capability.Supports(requestType) {
		return QualityUnsupported
	}
	if inboundFormat != "" && !formatCarriesOperation(inboundFormat, requestType) {
		return QualityUnsupported
	}
	if SupportsNativeFormat(outboundType, inboundFormat) {
		return QualityNative
	}
	if requestType == model.RequestTypeEmbedding &&
		inboundFormat == model.APIFormatOpenAIEmbedding &&
		capability.APIFormat == model.APIFormatOpenAIEmbedding {
		return QualityLossless
	}
	if requestType == model.RequestTypeChat || requestType == model.RequestTypeResponses {
		return QualityConditional
	}
	return QualityUnsupported
}

func formatCarriesOperation(format model.APIFormat, requestType model.RequestType) bool {
	switch requestType {
	case model.RequestTypeChat:
		return format == model.APIFormatOpenAIChatCompletion || format == model.APIFormatAnthropicMessage || format == model.APIFormatGeminiContents
	case model.RequestTypeResponses:
		return format == model.APIFormatOpenAIResponse
	case model.RequestTypeEmbedding:
		return format == model.APIFormatOpenAIEmbedding
	case model.RequestTypeImages:
		return format == model.APIFormatOpenAIImageGeneration
	default:
		return false
	}
}

type CapabilityMetricSnapshot struct {
	Total     uint64
	Supported uint64
	Degraded  uint64
	Rejected  uint64
	Losses    []LossMetric
}

type LossMetric struct {
	RequestType    model.RequestType
	InboundFormat  model.APIFormat
	OutboundFormat model.APIFormat
	Field          string
	Count          uint64
}

type lossMetricKey struct {
	requestType    model.RequestType
	inboundFormat  model.APIFormat
	outboundFormat model.APIFormat
	field          string
}

var capabilityMetrics struct {
	total     atomic.Uint64
	supported atomic.Uint64
	degraded  atomic.Uint64
	rejected  atomic.Uint64
	losses    sync.Map // lossMetricKey -> *atomic.Uint64
}

// RecordCapabilityDecision records aggregate transformation quality without
// retaining request data. The dimensions are bounded protocol, operation, and
// degraded-field identifiers produced by the planner.
func RecordCapabilityDecision(decision CapabilityDecision) {
	capabilityMetrics.total.Add(1)
	switch decision.Status {
	case CapabilitySupported:
		capabilityMetrics.supported.Add(1)
	case CapabilityDegraded:
		capabilityMetrics.degraded.Add(1)
	case CapabilityRejected:
		capabilityMetrics.rejected.Add(1)
	}
	metricFields := make([]string, 0, len(decision.DegradedFields))
	for _, field := range decision.DegradedFields {
		metricFields = append(metricFields, normalizeMetricField(field))
	}
	for _, field := range uniqueSorted(metricFields) {
		key := lossMetricKey{
			requestType:    decision.RequestType,
			inboundFormat:  decision.InboundFormat,
			outboundFormat: decision.OutboundFormat,
			field:          field,
		}
		counter, _ := capabilityMetrics.losses.LoadOrStore(key, &atomic.Uint64{})
		counter.(*atomic.Uint64).Add(1)
	}
}

func normalizeMetricField(field string) string {
	var normalized strings.Builder
	normalized.Grow(len(field))
	for index := 0; index < len(field); {
		if field[index] != '[' {
			normalized.WriteByte(field[index])
			index++
			continue
		}
		end := index + 1
		for end < len(field) && field[end] >= '0' && field[end] <= '9' {
			end++
		}
		if end > index+1 && end < len(field) && field[end] == ']' {
			normalized.WriteString("[]")
			index = end + 1
			continue
		}
		normalized.WriteByte(field[index])
		index++
	}
	return normalized.String()
}

func SnapshotCapabilityMetrics() CapabilityMetricSnapshot {
	snapshot := CapabilityMetricSnapshot{
		Total:     capabilityMetrics.total.Load(),
		Supported: capabilityMetrics.supported.Load(),
		Degraded:  capabilityMetrics.degraded.Load(),
		Rejected:  capabilityMetrics.rejected.Load(),
	}
	capabilityMetrics.losses.Range(func(key, value any) bool {
		metricKey := key.(lossMetricKey)
		snapshot.Losses = append(snapshot.Losses, LossMetric{
			RequestType:    metricKey.requestType,
			InboundFormat:  metricKey.inboundFormat,
			OutboundFormat: metricKey.outboundFormat,
			Field:          metricKey.field,
			Count:          value.(*atomic.Uint64).Load(),
		})
		return true
	})
	sort.Slice(snapshot.Losses, func(i, j int) bool {
		a, b := snapshot.Losses[i], snapshot.Losses[j]
		return strings.Join([]string{a.RequestType.String(), string(a.InboundFormat), string(a.OutboundFormat), a.Field}, "\x00") <
			strings.Join([]string{b.RequestType.String(), string(b.InboundFormat), string(b.OutboundFormat), b.Field}, "\x00")
	})
	return snapshot
}
