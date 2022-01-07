package telephonist

import (
	"syscall"
)

// A utility to convert the values to proper strings.
func int8ToStr(arr []int8) string {
	b := make([]byte, 0, len(arr))
	for _, v := range arr {
		if v == 0x00 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)

}

func getOsInfo() (string, error) {
	var uname syscall.Utsname
	var err error
	if err = syscall.Uname(&uname); err == nil {
		// extract members:
		// type Utsname struct {
		//  Sysname    [65]int8
		//  Nodename   [65]int8
		//  Release    [65]int8
		//  Version    [65]int8
		//  Machine    [65]int8
		//  Domainname [65]int8
		// }

		return int8ToStr(uname.Sysname[:]) + " " + int8ToStr(uname.Release[:]) + " " + int8ToStr(uname.Version[:]), nil

	}
	return "", err
}
