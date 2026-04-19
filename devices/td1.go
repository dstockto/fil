// Package devices provides access to USB peripherals that fil integrates with.
// Today that is the TD-1 color/transmission scanner.
package devices

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

const (
	// TD1VID is the USB vendor ID reported by the TD-1 (Raspberry Pi Pico).
	TD1VID = "E4B2"
	// TD1PID is the USB product ID reported by the TD-1.
	TD1PID = "0045"
	// TD1Baud is the serial baud rate configured on the TD-1 firmware.
	TD1Baud = 115200
)

// ErrNoDevice is returned by Probe when no TD-1 is attached.
var ErrNoDevice = errors.New("no TD-1 detected")

// PortInfo describes a detected TD-1 device.
type PortInfo struct {
	Path   string // OS device path, e.g. /dev/cu.usbmodem14101 (never cache — changes on reconnect)
	VID    string
	PID    string
	Serial string // USB serial number from the descriptor, not the TD-1's per-scan uid
}

// Port is the minimal serial I/O surface the scan loop needs. Having it as an
// interface lets tests script scripted CSV lines without real hardware.
type Port interface {
	ReadLine(ctx context.Context) (string, error)
	WriteLine(s string) error
	Close() error
}

// Enumerator locates TD-1 devices on the host. Extracted for testing.
type Enumerator interface {
	Find() ([]PortInfo, error)
}

// DefaultEnumerator is backed by go.bug.st/serial/enumerator and finds
// devices with the TD-1's VID:PID.
type DefaultEnumerator struct{}

// Find returns every attached TD-1. Multiple devices are supported but Probe
// currently selects the first one.
func (DefaultEnumerator) Find() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate serial ports: %w", err)
	}
	var out []PortInfo
	for _, p := range ports {
		if !p.IsUSB {
			continue
		}
		if strings.EqualFold(p.VID, TD1VID) && strings.EqualFold(p.PID, TD1PID) {
			out = append(out, PortInfo{
				Path:   p.Name,
				VID:    strings.ToUpper(p.VID),
				PID:    strings.ToUpper(p.PID),
				Serial: p.SerialNumber,
			})
		}
	}
	return out, nil
}

// Probe locates the first attached TD-1. Pass nil to use the default enumerator.
func Probe(e Enumerator) (PortInfo, error) {
	if e == nil {
		e = DefaultEnumerator{}
	}
	devs, err := e.Find()
	if err != nil {
		return PortInfo{}, err
	}
	if len(devs) == 0 {
		return PortInfo{}, ErrNoDevice
	}
	return devs[0], nil
}

// serialPort is the production Port implementation.
type serialPort struct {
	port serial.Port
	rd   *bufio.Reader
	mu   sync.Mutex
}

// Open opens the given device path at the TD-1's baud rate. The path should
// come from a fresh Probe — macOS renames serial device nodes on reconnect.
func Open(path string) (Port, error) {
	p, err := serial.Open(path, &serial.Mode{BaudRate: TD1Baud})
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", path, err)
	}
	return &serialPort{port: p, rd: bufio.NewReader(p)}, nil
}

// ReadLine reads a CRLF- or LF-terminated line. Cancelling ctx closes the
// port to unblock the underlying read; the returned error will be ctx.Err().
func (s *serialPort) ReadLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := s.rd.ReadString('\n')
		ch <- result{line: strings.TrimRight(line, "\r\n"), err: err}
	}()
	select {
	case <-ctx.Done():
		_ = s.port.Close()
		return "", ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

// WriteLine writes the given string, appending "\n" if not already present.
func (s *serialPort) WriteLine(msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	_, err := s.port.Write([]byte(msg))
	return err
}

// Close releases the serial port.
func (s *serialPort) Close() error {
	return s.port.Close()
}
