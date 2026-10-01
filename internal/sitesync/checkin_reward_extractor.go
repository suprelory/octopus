package sitesync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/fastschema/qjs"
)

const checkinRewardExtractorTimeout = 250 * time.Millisecond

var (
	checkinRewardExtractorSlots = make(chan struct{}, 2)
	errRewardExtractorFailed    = errors.New("reward extractor failed; check the JavaScript syntax, response fields and resource limits")
	errRewardExtractorTimeout   = errors.New("reward extractor exceeded its execution time limit")
	errRewardExtractorResult    = errors.New("reward extractor must return a finite non-negative number, a numeric string, or null")
)

// ExtractCheckinReward runs a synchronous function body with only a JSON response
// as input. Each call gets a fresh, memory-limited WASM runtime. No Go objects,
// credentials, HTTP clients, environment variables or host files are exposed.
func ExtractCheckinReward(ctx context.Context, code string, response json.RawMessage) (reward string, err error) {
	if err := model.ValidateCheckinRewardExtractor(code); err != nil {
		return "", err
	}
	if len(response) > siteCheckinResponseLimit || !json.Valid(response) {
		return "", errors.New("reward extractor response must be valid JSON no larger than 1 MiB")
	}
	if strings.TrimSpace(code) == "" {
		return "", nil
	}
	// Include queueing and cold WASM compilation in a separate bounded budget.
	initCtx, initCancel := context.WithTimeout(ctx, 5*time.Second)
	defer initCancel()
	select {
	case checkinRewardExtractorSlots <- struct{}{}:
		defer func() { <-checkinRewardExtractorSlots }()
	case <-initCtx.Done():
		return "", errRewardExtractorTimeout
	}
	defer func() {
		// wazero closes timed-out modules; qjs reports calls on them as panics.
		// Never return its stack trace or a script's thrown response data.
		if recover() != nil && err != errRewardExtractorTimeout {
			reward, err = "", errRewardExtractorFailed
		}
	}()
	runtime, err := qjs.New(qjs.Option{
		Context: initCtx, CloseOnContextDone: true,
		MemoryLimit: 16 << 20, MaxStackSize: 256 << 10,
		// qjs always mounts CWD. A null device is not a directory: every
		// guest path lookup fails, without creating a writable host directory.
		CWD: os.DevNull, Stdout: io.Discard, Stderr: io.Discard,
	})
	if err != nil {
		return "", errRewardExtractorFailed
	}
	defer func() {
		defer func() { _ = recover() }() // Already closed on cancellation.
		runtime.Context().Context = context.Background()
		runtime.Close()
	}()
	execCtx, execCancel := context.WithTimeout(initCtx, checkinRewardExtractorTimeout)
	defer execCancel()
	runtime.Context().Context = execCtx
	defer func() {
		if execCtx.Err() != nil {
			reward, err = "", errRewardExtractorTimeout
		}
	}()
	// Eval is used only to compile the body and remove qjs host helpers. The
	// body is a quoted string, so it cannot execute during this step.
	body, _ := json.Marshal("\"use strict\";\n" + code)
	function, err := runtime.Eval("checkin-reward.js", qjs.Code(`
		for (const key of ["std", "os", "bjson", "print", "console", "scriptArgs",
			"setTimeout", "setInterval", "clearTimeout", "clearInterval", "QJS_PROXY_VALUE"]) {
			delete globalThis[key];
		}
		new Function("response", `+string(body)+`);
	`))
	if err != nil {
		return "", errRewardExtractorFailed
	}
	defer function.Free()
	js := runtime.Context()
	input := js.ParseJSON(string(response))
	defer input.Free()
	if js.HasException() {
		return "", errRewardExtractorFailed
	}
	this := js.NewUndefined()
	defer this.Free()
	// Invoke calls JS_Call directly. Do not use Eval or pump pending jobs after
	// running user code: asynchronous imports/timers are outside this contract.
	value, err := js.Invoke(function, this, input)
	if err != nil {
		return "", errRewardExtractorFailed
	}
	defer value.Free()
	if value.IsNull() || value.IsUndefined() {
		return "", nil
	}
	var number float64
	switch {
	case value.IsNumber():
		number = value.Float64()
	case value.IsString():
		result := strings.TrimSpace(value.String())
		if len(result) > 128 {
			return "", errRewardExtractorResult
		}
		number, err = strconv.ParseFloat(result, 64)
		if err != nil {
			return "", errRewardExtractorResult
		}
	default:
		return "", errRewardExtractorResult
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return "", errRewardExtractorResult
	}
	reward = strings.TrimRight(strings.TrimRight(strconv.FormatFloat(number, 'f', 6, 64), "0"), ".")
	if reward == "-0" {
		reward = "0"
	}
	return reward, nil
}
