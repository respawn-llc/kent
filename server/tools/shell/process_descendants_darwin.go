//go:build darwin

package shell

import "golang.org/x/sys/unix"

func managedProcessSnapshot() (map[int]managedProcessSnapshotEntry, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	snapshot := make(map[int]managedProcessSnapshotEntry, len(processes))
	for _, process := range processes {
		snapshot[int(process.Proc.P_pid)] = managedProcessSnapshotEntry{
			parentPID:      int(process.Eproc.Ppid),
			processGroupID: int(process.Eproc.Pgid),
			startedAt: uint64(process.Proc.P_starttime.Sec)*1_000_000 +
				uint64(process.Proc.P_starttime.Usec),
			// sysctl exposes P_LEXIT as P_WEXIT before the process reaches SZOMB.
			exited: process.Proc.P_stat == 5 || process.Proc.P_flag&0x00002000 != 0,
		}
	}
	return snapshot, nil
}
