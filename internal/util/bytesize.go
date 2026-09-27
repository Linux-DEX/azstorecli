package util

import (
	"time"

	"github.com/dustin/go-humanize"
)

// Bytes renders a size the way a human scans a column: "4.2 MB".
func Bytes(n int64) string {
	if n < 0 {
		return "—"
	}
	return humanize.Bytes(uint64(n))
}

// RelTime renders a timestamp as "3 minutes ago", or "—" when zero.
func RelTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return humanize.Time(t)
}

// Duration renders an uptime as "12m 04s" / "3h 12m".
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return formatHM(h, m)
	case m > 0:
		return formatMS(m, s)
	default:
		return formatS(s)
	}
}

func formatHM(h, m int) string { return itoa(h) + "h " + pad2(m) + "m" }
func formatMS(m, s int) string { return itoa(m) + "m " + pad2(s) + "s" }
func formatS(s int) string     { return itoa(s) + "s" }

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
