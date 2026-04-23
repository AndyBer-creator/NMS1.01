// Package mibresolver resolves symbolic MIB names (e.g. IF-MIB::sysDescr.0)
// into numeric OIDs using external snmptranslate (net-snmp-tools).
package mibresolver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var numericOID = regexp.MustCompile(`^\.?(\d+)(\.\d+)*$`)

// IsNumericOID reports whether input is numeric OID with optional leading dot.
func IsNumericOID(s string) bool {
	s = strings.TrimSpace(s)
	return numericOID.MatchString(s)
}

// NormalizeNumeric strips leading dot from numeric OID.
func NormalizeNumeric(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, ".")
}

var safeSymbol = regexp.MustCompile(`^[A-Za-z0-9_.:\-]+$`)

// ValidateSymbol guards snmptranslate argument from invalid characters.
func ValidateSymbol(s string) error {
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > 512 {
		return errors.New("invalid symbol length")
	}
	if !safeSymbol.MatchString(s) {
		return errors.New("symbol contains forbidden characters (allowed: letters, digits, . _ - :)")
	}
	return nil
}

// Resolver executes snmptranslate using provided MIB directories.
type Resolver struct {
	dirs            []string
	logger          *zap.Logger
	mu              sync.Mutex
	translateBin    string
	translateChecked bool
}

func New(dirs []string, logger *zap.Logger) *Resolver {
	var clean []string
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		clean = append(clean, filepath.Clean(d))
	}
	clean = appendStandardMIBDirs(clean)
	return &Resolver{dirs: clean, logger: logger}
}

func appendStandardMIBDirs(dirs []string) []string {
	seen := make(map[string]bool)
	for _, d := range dirs {
		seen[d] = true
	}
	for _, s := range []string{"/usr/share/snmp/mibs", "/usr/local/share/snmp/mibs"} {
		if st, err := os.Stat(s); err == nil && st.IsDir() && !seen[s] {
			dirs = append(dirs, s)
			seen[s] = true
		}
	}
	return dirs
}

func (r *Resolver) mibDirsEnv() string {
	return strings.Join(r.dirs, string(os.PathListSeparator))
}

func (r *Resolver) findTranslate() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.translateChecked {
		if r.translateBin == "" {
			return "", errors.New("snmptranslate not found in PATH")
		}
		return r.translateBin, nil
	}
	r.translateChecked = true
	path, err := exec.LookPath("snmptranslate")
	if err != nil {
		r.logger.Warn("snmptranslate not found; symbolic MIB names disabled until net-snmp-tools is installed")
		return "", err
	}
	r.translateBin = path
	r.logger.Info("MIB resolver: snmptranslate", zap.String("path", path), zap.String("MIBDIRS", r.mibDirsEnv()))
	return path, nil
}

// ResolveToNumeric resolves symbolic or numeric input to normalized numeric OID.
func (r *Resolver) ResolveToNumeric(symbol string) (string, error) {
	s := strings.TrimSpace(symbol)
	if IsNumericOID(s) {
		return NormalizeNumeric(s), nil
	}
	if err := ValidateSymbol(s); err != nil {
		return "", err
	}
	bin, err := r.findTranslate()
	if err != nil {
		return "", fmt.Errorf("MIB resolver unavailable: %w (install net-snmp-tools and place MIB files under configured dirs)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-On", "-IR", s)
	cmd.Env = append(os.Environ(),
		"MIBDIRS="+r.mibDirsEnv(),
		"MIBS=+ALL",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("snmptranslate: %s", msg)
		}
		return "", fmt.Errorf("snmptranslate: %w", err)
	}
	line := strings.TrimSpace(string(out))
	line = strings.TrimPrefix(line, ".")
	line = NormalizeNumeric(line)
	if !IsNumericOID(line) {
		return "", fmt.Errorf("snmptranslate returned unexpected output: %q", strings.TrimSpace(string(out)))
	}
	return line, nil
}

// Available reports whether symbolic resolution is available.
func (r *Resolver) Available() bool {
	_, err := r.findTranslate()
	return err == nil
}

// PickSNMPValue returns value by numeric OID handling dot/no-dot key variants.
func PickSNMPValue(result map[string]string, numericOID string) string {
	numericOID = NormalizeNumeric(numericOID)
	if v, ok := result[numericOID]; ok {
		return v
	}
	if v, ok := result["."+numericOID]; ok {
		return v
	}
	for k, v := range result {
		if NormalizeNumeric(k) == numericOID {
			return v
		}
	}
	if len(result) == 1 {
		for _, v := range result {
			return v
		}
	}
	return ""
}
