package logger

import (
	"fmt"
	"math"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestGetRealClientIP_UsesXRealIPHeader(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs(nil))

	const xRealIPClientIP = "111.111.111.111"
	const directPeerIP = "10.10.10.10"

	request := &fasthttp.Request{}
	request.Header.Set("X-Real-IP", xRealIPClientIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(directPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, xRealIPClientIP, actualClientIP)
}

func TestGetRealClientIP_UsesRightmostXForwardedForWithoutTrustedProxyConfig(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs(nil))

	const originalClientIP = "111.111.111.111"
	const closestProxyIP = "222.222.222.222"
	const directPeerIP = "10.10.10.10"

	request := &fasthttp.Request{}
	request.Header.Set("X-Forwarded-For", originalClientIP+", "+closestProxyIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(directPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, closestProxyIP, actualClientIP)
}

func TestGetRealClientIP_UsesPeerAddressWhenPeerIsNotTrustedProxy(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16"}))

	const spoofedOriginalClientIP = "198.51.100.77"
	const intermediateProxyIP = "203.0.113.9"
	const trustedLoadBalancerIP = "34.96.120.5"
	const directPeerIP = "10.10.10.10"

	request := &fasthttp.Request{}
	request.Header.Set("X-Forwarded-For", spoofedOriginalClientIP+", "+intermediateProxyIP+", "+trustedLoadBalancerIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(directPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, directPeerIP, actualClientIP)
}

func TestGetRealClientIP_UsesFirstUntrustedAddressFromRightWhenPeerIsTrusted(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16", "162.159.0.0/16"}))

	const originalClientIP = "198.51.100.77"
	const untrustedIntermediateProxyIP = "203.0.113.9"
	const trustedEdgeProxyIP = "162.159.1.10"
	const trustedLoadBalancerIP = "34.96.120.5"

	request := &fasthttp.Request{}
	request.Header.Set("X-Forwarded-For", originalClientIP+", "+untrustedIntermediateProxyIP+", "+trustedEdgeProxyIP)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP(trustedLoadBalancerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)

	require.Equal(t, untrustedIntermediateProxyIP, actualClientIP)
}

func TestInitLogger_DevMode(t *testing.T) {
	err := InitLogger(true, "debug", math.MaxInt, math.MaxInt, []string{"127.0.0.1/32"})
	require.NoError(t, err)
	require.NotNil(t, Logger)
}

func TestInitLogger_ProdMode(t *testing.T) {
	err := InitLogger(false, "info", 100, 100, nil)
	require.NoError(t, err)
	require.NotNil(t, Logger)
}

func TestInitLogger_InvalidLevel(t *testing.T) {
	err := InitLogger(false, "invalid-level", math.MaxInt, math.MaxInt, nil)
	require.Error(t, err)
}

func TestInitLogger_InvalidCIDR(t *testing.T) {
	err := InitLogger(false, "info", math.MaxInt, math.MaxInt, []string{"invalid-cidr"})
	require.Error(t, err)
}

func TestSetDetails(t *testing.T) {
	var fhctx fasthttp.RequestCtx
	SetDetails(&fhctx, zapcore.InfoLevel, "test-msg", fmt.Errorf("test-err"), []zap.Field{zap.String("key", "val")})

	require.Equal(t, zapcore.InfoLevel, fhctx.UserValue("level"))
	require.Equal(t, "test-msg", fhctx.UserValue("msg"))
	require.Equal(t, fmt.Errorf("test-err"), fhctx.UserValue("error"))
	require.Equal(t, []zap.Field{zap.String("key", "val")}, fhctx.UserValue("zap_fields"))
}

func TestParseIPAddress_EdgeCases(t *testing.T) {
	require.Nil(t, parseIPAddress(""))
	require.Nil(t, parseIPAddress("   "))
	require.Nil(t, parseIPAddress("invalid-ip"))
	require.Nil(t, parseIPAddress("invalid-ip:8080")) // SplitHostPort success but ParseIP host fails
	// Valid ip with port
	ip := parseIPAddress("1.2.3.4:80")
	require.Equal(t, "1.2.3.4", ip.String())

	// IPv6 support
	ipv6 := parseIPAddress("2001:db8::1")
	require.Equal(t, "2001:db8::1", ipv6.String())

	ipv6WithPort := parseIPAddress("[2001:db8::1]:8080")
	require.Equal(t, "2001:db8::1", ipv6WithPort.String())

	ipv6Local := parseIPAddress("[::1]:80")
	require.Equal(t, "::1", ipv6Local.String())
}

func TestGetRealClientIP_UnparseablePeerAddress(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs(nil))
	request := &fasthttp.Request{}
	remoteAddr := &invalidAddr{}
	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	require.Equal(t, "invalid-addr", actualClientIP)
}

func TestGetRealClientIP_NilRemoteAddress(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs(nil))
	request := &fasthttp.Request{}
	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, nil, nil)

	actualClientIP := getRealClientIP(&fhctx)
	require.Equal(t, "0.0.0.0", actualClientIP)
}

type invalidAddr struct{}

func (a *invalidAddr) Network() string { return "tcp" }
func (a *invalidAddr) String() string  { return "invalid-addr" }

func TestGetRealClientIP_AllProxiesTrusted(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16", "162.159.0.0/16"}))
	request := &fasthttp.Request{}
	request.Header.Set("X-Forwarded-For", "34.96.1.1, 162.159.1.1")
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("34.96.2.2"), Port: 12345}
	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	require.Equal(t, "34.96.2.2", actualClientIP)
}

func TestGetRealClientIP_PrecedenceNoTrustedProxies(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs(nil))

	const xRealIP = "1.2.3.4"
	const xff = "5.6.7.8, 9.10.11.12"
	const peerIP = "13.14.15.16"

	request := &fasthttp.Request{}
	request.Header.Set("X-Real-IP", xRealIP)
	request.Header.Set("X-Forwarded-For", xff)
	remoteAddr := &net.TCPAddr{IP: net.ParseIP(peerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	// Without trusted proxies, X-Real-IP should take precedence over X-Forwarded-For.
	require.Equal(t, xRealIP, actualClientIP)
}

func TestGetRealClientIP_UsesXRealIPWhenPeerIsTrusted(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16"}))

	const xRealIP = "198.51.100.77" // untrusted client IP
	const trustedPeerIP = "34.96.120.5"

	request := &fasthttp.Request{}
	request.Header.Set("X-Real-IP", xRealIP)
	remoteAddr := &net.TCPAddr{IP: net.ParseIP(trustedPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	require.Equal(t, xRealIP, actualClientIP)
}

func TestGetRealClientIP_UsesPeerAddressWhenXRealIPIsTrusted(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16"}))

	const trustedXRealIP = "34.96.1.1" // trusted proxy IP
	const trustedPeerIP = "34.96.120.5"

	request := &fasthttp.Request{}
	request.Header.Set("X-Real-IP", trustedXRealIP)
	remoteAddr := &net.TCPAddr{IP: net.ParseIP(trustedPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	// Since the X-Real-IP is also a trusted proxy, there are no untrusted client IPs.
	// We fall back to the peer IP address.
	require.Equal(t, trustedPeerIP, actualClientIP)
}

func TestGetRealClientIP_MalformedXForwardedFor(t *testing.T) {
	require.NoError(t, setTrustedProxyCIDRs([]string{"34.96.0.0/16"}))

	const untrustedClientIP = "198.51.100.77"
	const trustedPeerIP = "34.96.120.5"

	request := &fasthttp.Request{}
	// Malformed intermediate segments like spaces, garbage, etc.
	request.Header.Set("X-Forwarded-For", untrustedClientIP+", malformed_ip, 34.96.1.1")
	remoteAddr := &net.TCPAddr{IP: net.ParseIP(trustedPeerIP), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)

	actualClientIP := getRealClientIP(&fhctx)
	// "malformed_ip" should be skipped, so the parsed list is [untrustedClientIP, 34.96.1.1].
	// Going right-to-left: 34.96.1.1 (trusted) -> untrustedClientIP (untrusted).
	// Therefore, untrustedClientIP is returned.
	require.Equal(t, untrustedClientIP, actualClientIP)
}

func TestLogRequest(t *testing.T) {
	// Create observer core
	core, logs := observer.New(zapcore.DebugLevel)
	oldLogger := Logger
	Logger = zap.New(core)
	defer func() { Logger = oldLogger }()

	request := &fasthttp.Request{}
	request.Header.SetMethod("GET")
	request.Header.Set("User-Agent", "test-agent")
	request.Header.SetContentType("application/json")
	request.SetRequestURI("/test-path?param=1")
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 12345}

	var fhctx fasthttp.RequestCtx
	fhctx.Init(request, remoteAddr, nil)
	fhctx.Response.SetStatusCode(200)
	fhctx.Response.SetBody([]byte("hello"))

	LogRequest(&fhctx)

	require.Equal(t, 1, logs.Len())
	logEntry := logs.All()[0]
	require.Equal(t, zapcore.ErrorLevel, logEntry.Level)
	require.Equal(t, "", logEntry.Message)

	fields := logEntry.ContextMap()
	require.Equal(t, "1.2.3.4", fields["client_ip"])
	require.Equal(t, "GET", fields["http_method"])
	require.Equal(t, int64(200), fields["http_status"])
	require.Equal(t, "test-agent", fields["user_agent"])
	require.Equal(t, "application/json", fields["request_content_type"])
	require.Equal(t, "HTTP/1.1", fields["protocol"])
	require.Equal(t, "/test-path?param=1", fields["raw_path"])
	require.Equal(t, int64(5), fields["response_body_size"])
	require.Contains(t, fields, "time_taken_ns")
}

func TestLogRequest_CustomFieldsAndLevels(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	oldLogger := Logger
	Logger = zap.New(core)
	defer func() { Logger = oldLogger }()

	levels := []zapcore.Level{zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel}

	for _, lvl := range levels {
		logs.TakeAll()
		var fhctx fasthttp.RequestCtx
		request := &fasthttp.Request{}
		remoteAddr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 12345}
		fhctx.Init(request, remoteAddr, nil)

		err := fmt.Errorf("some-error")
		extraFields := []zap.Field{zap.String("custom_key", "custom_val")}
		SetDetails(&fhctx, lvl, "custom-msg", err, extraFields)

		LogRequest(&fhctx)

		require.Equal(t, 1, logs.Len())
		logEntry := logs.All()[0]
		require.Equal(t, lvl, logEntry.Level)
		require.Equal(t, "custom-msg", logEntry.Message)

		fields := logEntry.ContextMap()
		require.Equal(t, "some-error", fields["error"])
		require.Equal(t, "custom_val", fields["custom_key"])
	}
}
