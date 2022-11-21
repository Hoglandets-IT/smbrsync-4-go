package main

import (
	"fmt"
	"net"
	"time"

	"github.com/Hoglandets-IT/smbrsync-4-go/smbrsync"
	"github.com/hirochachacha/go-smb2"
)

func main() {
	// Start timer
	start := time.Now()
	fmt.Println("Starting sync")
	src_conn, err1 := net.Dial("tcp", "RGB-BOX:445")
	if err1 != nil {
		panic(err1)
	}
	defer src_conn.Close()

	dst_conn, err2 := net.Dial("tcp", "RGB-BOX:445")
	if err2 != nil {
		panic(err2)
	}
	defer dst_conn.Close()

	credentials := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     "",
			Password: "",
			Domain:   "",
		},
	}

	src, serr := credentials.Dial(src_conn)
	if serr != nil {
		panic(serr)
	}
	defer src.Logoff()

	dst, derr := credentials.Dial(dst_conn)
	if derr != nil {
		panic(derr)
	}
	defer dst.Logoff()

	srcsh, err := src.Mount("src$")
	if err != nil {
		panic(err)
	}
	defer srcsh.Umount()

	dstsh, err := dst.Mount("dst$")
	if err != nil {
		panic(err)
	}
	defer dstsh.Umount()

	fmt.Println("Servers connected, starting synchronization at ", time.Since(start).Seconds(), " sec after start")

	filters := []string{
		`whitelist.json`,
		`^world\\level.dat$`,
	}

	sync, err := smbrsync.New(
		&smbrsync.SmbRsyncShare{
			Share:    srcsh,
			BasePath: "somedir",
		},

		&smbrsync.SmbRsyncShare{
			Share:    dstsh,
			BasePath: "someotherdir",
		},

		filters,
	)
	if err != nil {
		panic(err)
	}

	result, err := sync.Sync()
	if err != nil {
		panic(err)
	}

	defer println("Finished processing")

	end := time.Now()
	fmt.Println("Total runtime: ", end.Sub(start).Seconds(), " seconds")

	//fmt.Println("Skipped: ", result.Skipped)
	fmt.Println("Excluded: ", result.Excluded)
	fmt.Println("Copied: ", result.Copied)
	fmt.Println("Mismatch: ", result.Mismatch)
	fmt.Println("Deleted: ", result.Deleted)
}
