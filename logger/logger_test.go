package logger

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
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
