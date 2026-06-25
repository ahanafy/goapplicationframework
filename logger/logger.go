package logger

import (
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/robstradling/goapplicationframework/utils"

	"github.com/valyala/fasthttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

var trustedProxyCIDRs []*net.IPNet

func InitLogger(isDevelopment bool, level string, samplingInitial int, samplingThereafter int, trustedProxyCIDRValues []string) error {
	// Create and configure a Zap logger.
	var err error
	var cfg zap.Config
	if isDevelopment {
		cfg = zap.NewDevelopmentConfig() // "debug" and above; console-friendly output.
	} else {
		cfg = zap.NewProductionConfig() // "info" and above; JSON output.
		cfg.DisableCaller = true
	}
	// Override log level threshold, if required.
	if level != "" {
		if cfg.Level, err = zap.ParseAtomicLevel(level); err != nil {
			return err
		}
	}
	// Configure or disable log sampling.
	if samplingInitial == math.MaxInt && samplingThereafter == math.MaxInt {
		cfg.Sampling = nil // Disable sampling.
	} else {
		cfg.Sampling = &zap.SamplingConfig{
			Initial:    samplingInitial,
			Thereafter: samplingThereafter,
		}
	}
	// Configure timestamp format.
	cfg.EncoderConfig.TimeKey = "@timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeDuration = zapcore.NanosDurationEncoder

	if err = setTrustedProxyCIDRs(trustedProxyCIDRValues); err != nil {
		return err
	}

	Logger, err = cfg.Build()
	return err
}

func setTrustedProxyCIDRs(cidrValues []string) error {
	trustedProxyCIDRs = nil
	for _, cidrValue := range cidrValues {
		cidrValue = strings.TrimSpace(cidrValue)
		if cidrValue == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidrValue)
		if err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidrValue, err)
		}
		trustedProxyCIDRs = append(trustedProxyCIDRs, ipNet)
	}
	return nil
}

func isTrustedProxyIP(ipAddress net.IP) bool {
	for _, trustedProxyCIDR := range trustedProxyCIDRs {
		if trustedProxyCIDR.Contains(ipAddress) {
			return true
		}
	}
	return false
}

func parseIPAddress(value string) net.IP {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if ipAddress := net.ParseIP(value); ipAddress != nil {
		return ipAddress
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

func parseXForwardedForHeader(xffValue string) []net.IP {
	parts := strings.Split(xffValue, ",")
	ipAddresses := make([]net.IP, 0, len(parts))
	for _, part := range parts {
		if ipAddress := parseIPAddress(part); ipAddress != nil {
			ipAddresses = append(ipAddresses, ipAddress)
		}
	}
	return ipAddresses
}

func SetDetails(fhctx *fasthttp.RequestCtx, level zapcore.Level, msg string, err error, extraFields []zap.Field) {
	fhctx.SetUserValue("level", level)
	fhctx.SetUserValue("msg", msg)
	if err != nil {
		fhctx.SetUserValue("error", err)
	}
	if extraFields != nil {
		fhctx.SetUserValue("zap_fields", extraFields)
	}
}

func getRealClientIP(fhctx *fasthttp.RequestCtx) string {
	peerIPAddress := parseIPAddress(fhctx.RemoteAddr().String())
	if peerIPAddress == nil {
		return fhctx.RemoteAddr().String()
	}

	if len(trustedProxyCIDRs) == 0 {
		if realIP := fhctx.Request.Header.Peek("X-Real-IP"); len(realIP) > 0 {
			if ipAddress := parseIPAddress(utils.B2S(realIP)); ipAddress != nil {
				return ipAddress.String()
			}
		} else if xff := fhctx.Request.Header.Peek("X-Forwarded-For"); len(xff) > 0 {
			ipAddresses := parseXForwardedForHeader(utils.B2S(xff))
			if len(ipAddresses) > 0 {
				return ipAddresses[len(ipAddresses)-1].String()
			}
		}
		return peerIPAddress.String()
	}

	if !isTrustedProxyIP(peerIPAddress) {
		return peerIPAddress.String()
	}

	var forwardedIPs []net.IP
	if xff := fhctx.Request.Header.Peek("X-Forwarded-For"); len(xff) > 0 {
		forwardedIPs = parseXForwardedForHeader(utils.B2S(xff))
	} else if realIP := fhctx.Request.Header.Peek("X-Real-IP"); len(realIP) > 0 {
		if ipAddress := parseIPAddress(utils.B2S(realIP)); ipAddress != nil {
			forwardedIPs = append(forwardedIPs, ipAddress)
		}
	}

	for i := len(forwardedIPs) - 1; i >= 0; i-- {
		if !isTrustedProxyIP(forwardedIPs[i]) {
			return forwardedIPs[i].String()
		}
	}

	return peerIPAddress.String()
}

func LogRequest(fhctx *fasthttp.RequestCtx) {
	// Add common logging details.
	zf := []zap.Field{
		zap.String("client_ip", getRealClientIP(fhctx)),
		zap.ByteString("http_method", fhctx.Method()),
		zap.Int("http_status", fhctx.Response.StatusCode()),
		zap.ByteString("protocol", fhctx.Request.Header.Protocol()),
		zap.ByteString("raw_path", fhctx.RequestURI()),
		zap.Int("response_body_size", len(fhctx.Response.Body())),
		zap.Duration("time_taken_ns", time.Since(fhctx.Time())),
	}

	// Add further optional logging details.
	if e := fhctx.UserValue("error"); e != nil {
		zf = append(zf, zap.Error(e.(error)))
	}
	if ct := fhctx.Request.Header.ContentType(); len(ct) > 0 {
		zf = append(zf, zap.ByteString("request_content_type", ct))
	}
	if ua := fhctx.Request.Header.UserAgent(); len(ua) > 0 {
		zf = append(zf, zap.ByteString("user_agent", ua))
	}

	// Add application-specific details.
	if f := fhctx.UserValue("zap_fields"); f != nil {
		zf = append(zf, f.([]zapcore.Field)...)
	}

	// Get the error level and message.
	level := zap.ErrorLevel
	if l := fhctx.UserValue("level"); l != nil {
		level = l.(zapcore.Level)
	}

	msg := ""
	if m := fhctx.UserValue("msg"); m != nil {
		msg = m.(string)
	}

	// Write the log entry.
	switch level {
	case zap.ErrorLevel:
		Logger.Error(msg, zf...)
	case zap.WarnLevel:
		Logger.Warn(msg, zf...)
	case zap.InfoLevel:
		Logger.Info(msg, zf...)
	case zap.DebugLevel:
		Logger.Debug(msg, zf...)
	}
}
