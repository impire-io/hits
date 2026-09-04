package node

import (
	"fmt"
	"strconv"
	"strings"
)

// The default byte budgets (hits-hq decisions 0005 and 0012). Every
// resource HITS creates declares one — some accounts (Synadia Cloud among
// them) require it, and HITS declares them everywhere so there is one
// shape. The ops stream refuses new writes at its cap rather than trimming
// history: a budget bounds growth, never memory.
const (
	// DefaultMaxBytes is the ops stream's budget when Config leaves it 0.
	DefaultMaxBytes = 1 << 30 // 1 GiB
)

// Config tunes the resources Start provisions. The zero value is the
// decided default shape.
type Config struct {
	// MaxBytes is the ops stream's byte budget; 0 means DefaultMaxBytes.
	// The hits-state bucket scales with it at a quarter of the budget.
	MaxBytes int64
}

func (c Config) opsMaxBytes() int64 {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return DefaultMaxBytes
}

func (c Config) stateMaxBytes() int64 { return c.opsMaxBytes() / 4 }

// ParseSize reads a human byte size: plain bytes, or a K/M/G/T suffix in
// base 1024 ("512M", "2G"). The form the --max-bytes flags accept.
func ParseSize(s string) (int64, error) {
	in := strings.TrimSpace(s)
	if in == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := int64(1)
	switch strings.ToUpper(in[len(in)-1:]) {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "G":
		mult = 1 << 30
	case "T":
		mult = 1 << 40
	}
	num := in
	if mult > 1 {
		num = strings.TrimSpace(in[:len(in)-1])
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("not a size: %q (want a positive number, optionally suffixed K, M, G, or T)", s)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q overflows", s)
	}
	return n * mult, nil
}
