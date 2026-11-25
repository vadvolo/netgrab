package connector

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestDeviceConnection exercises a real SSH session against a live device. It is
// skipped unless NETGRAB_TEST_DEVICE_ADDR is set, so the default `go test` run
// stays hermetic. Provide the target via the environment:
//
//	NETGRAB_TEST_DEVICE_ADDR=192.0.2.1 \
//	NETGRAB_TEST_DEVICE_USER=admin \
//	NETGRAB_TEST_DEVICE_PASS=secret \
//	go test ./internal/connector -run TestDeviceConnection
func TestDeviceConnection(t *testing.T) {
	addr := os.Getenv("NETGRAB_TEST_DEVICE_ADDR")
	if addr == "" {
		t.Skip("set NETGRAB_TEST_DEVICE_ADDR (and _USER/_PASS) to run the live device test")
	}

	name := os.Getenv("NETGRAB_TEST_DEVICE_NAME")
	if name == "" {
		name = "test-device"
	}

	device := NewCiscoDevice(
		name,
		os.Getenv("NETGRAB_TEST_DEVICE_USER"),
		addr,
		os.Getenv("NETGRAB_TEST_DEVICE_PASS"),
	)
	observedZapCore, _ := observer.New(zap.InfoLevel)
	logger := zap.New(observedZapCore)

	require.NoError(t, device.Connect(logger))
	_, err := device.GetConfig(context.Background())
	require.NoError(t, err)
}
