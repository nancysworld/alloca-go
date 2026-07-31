package loadgen

import (
	"syscall"
	"time"
)

// cpuSample reads the generator process's consumed CPU time.
//
// It exists so generator utilisation is *measured* rather than inferred. §6.3 requires the
// harness to expose enough of its own utilisation to rule out generator saturation, and
// "the run took N seconds so the generator was fine" is exactly the reasoning that lets a
// client-limited plateau be published as server capacity.
func cpuSample() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return tv(ru.Utime) + tv(ru.Stime)
}

// cpuDelta reports CPU seconds consumed since start.
func cpuDelta(start time.Duration) float64 {
	now := cpuSample()
	if now < start {
		return 0
	}
	return (now - start).Seconds()
}

func tv(t syscall.Timeval) time.Duration {
	return time.Duration(t.Sec)*time.Second + time.Duration(t.Usec)*time.Microsecond
}
