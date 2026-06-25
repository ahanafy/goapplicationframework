package logger

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestGetRealClientIP_UsesXRealIPHeader(t *testing.T) {
	t.Parallel()

	const xRealIPClientIP = "198.51.100.42"
	const directPeerIP = "203.0.113.10"

	request := &fasthttp.Request{}
	request.Header.Set("X-Real-IP", xRealIPClientIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(directPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, xRealIPClientIP, actualClientIP)
}

func TestGetRealClientIP_UsesLeftmostXForwardedForClientIP(t *testing.T) {
	t.Parallel()

	// MDN X-Forwarded-For semantics:
	// - Leftmost IP is the originating client address.
	// - Rightmost IP is the most recent proxy.
	// Ref: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/X-Forwarded-For
	const originalClientIP = "2.2.2.2"
	const closestProxyIP = "1.1.1.1"
	const directPeerIP = "10.10.10.10"

	request := &fasthttp.Request{}
	request.Header.Set("X-Forwarded-For", originalClientIP+", "+closestProxyIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(directPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, originalClientIP, actualClientIP)
}

func TestSetDetails_SetsUserValues(t *testing.T) {
	t.Parallel()

	req := &fasthttp.Request{}
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(req, remote, nil)

	extra := []zap.Field{zap.String("k", "v")}
	SetDetails(&fhctx, zap.InfoLevel, "mymessage", errors.New("oops"), extra)

	require.Equal(t, zap.InfoLevel, fhctx.UserValue("level"))
	require.Equal(t, "mymessage", fhctx.UserValue("msg"))
	require.NotNil(t, fhctx.UserValue("error"))
	require.Equal(t, extra, fhctx.UserValue("zap_fields"))
}

func TestLogRequest_ProducesLogEntryWithFields(t *testing.T) {
	t.Parallel()

	// use an observed core so we can inspect emitted log entries
	core, obs := observer.New(zapcore.DebugLevel)
	Logger = zap.New(core)

	req := &fasthttp.Request{}
	req.Header.SetContentType("application/json")
	req.Header.SetUserAgent("tester")
	req.SetRequestURI("/test/path")
	req.Header.SetProtocol("HTTP/1.1")
	req.Header.SetMethod("GET")

	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 54321}
	var fhctx fasthttp.RequestCtx
	fhctx.Init(req, remote, nil)
	fhctx.Response.SetStatusCode(200)
	fhctx.Response.SetBodyString("ok")

	// add meaningful details and an extra field we can assert on
	extra := []zap.Field{zap.String("extra_key", "extra_val")}
	SetDetails(&fhctx, zap.InfoLevel, "hello-world", nil, extra)

	LogRequest(&fhctx)

	entries := obs.All()
	require.GreaterOrEqual(t, len(entries), 1)

	// find our entry (message should match)
	var e zapcore.Entry
	var ctx []zapcore.Field
	found := false
	for _, ent := range entries {
		if ent.Message == "hello-world" {
			e = ent.Entry
			ctx = ent.Context
			found = true
			break
		}
	}
	require.True(t, found, "expected a log entry with message 'hello-world'")
	require.Equal(t, zap.InfoLevel, e.Level)

	// assert that our extra field appears in the context
	seen := map[string]bool{}
	for _, f := range ctx {
		seen[f.Key] = true
	}
	require.Contains(t, seen, "extra_key")
}

func TestInitLogger_InvalidLevel_ReturnsError(t *testing.T) {
	t.Parallel()

	// invalid level should return an error
	err := InitLogger(false, "not-a-level", 1, 1)
	require.Error(t, err)
}

func TestInitLogger_ValidLevel_EnablesGivenLevel(t *testing.T) {
	t.Parallel()

	// set debug level and ensure the logger core reports Debug enabled
	err := InitLogger(false, "debug", 1, 1)
	require.NoError(t, err)
	require.NotNil(t, Logger)
	// core should have debug enabled
	require.True(t, Logger.Core().Enabled(zapcore.DebugLevel))
}

func TestInitLogger_DisableSampling_Works(t *testing.T) {
	t.Parallel()

	// passing MaxInt for both sampling values disables sampling configuration
	err := InitLogger(true, "info", int(^uint(0)>>1), int(^uint(0)>>1))
	require.NoError(t, err)
	require.NotNil(t, Logger)
}
