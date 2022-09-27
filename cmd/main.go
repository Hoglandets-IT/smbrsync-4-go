package main

import (
	"crypto/md5"
	"fmt"
	"io/fs"
	"net"
	"strings"
	"time"

	"github.com/Hoglandets-IT/smbrsync-4-go/smbrsync"
)


func main() {
	// Start timer
	start := time.Now()
	fmt.Println("Starting sync")
	src_conn, err1 := net.Dial("tcp", "some-server:445")
	if err1 != nil {
		panic(err1)
	}
	defer src_conn.Close()

	dst_conn, err2 := net.Dial("tcp", "other-server:445")
	if err2 != nil {
		panic(err2)
	}
	defer dst_conn.Close()

	credentials := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     "username",
			Password: "password",
			Domain:   "domain",
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

	srcsh, err := src.Mount("SomeShare$")
	if err != nil {
		panic(err)
	}
	defer srcsh.Umount()

	dstsh, err := dst.Mount("OtherShare$")
	if err != nil {
		panic(err)
	}
	defer dstsh.Umount()

	sdir := "rsync_src"
	ddir := "rsync_dst"

	fmt.Println("Servers connected, starting synchronization at ", time.Since(start).Seconds(), " sec after start")

	reco_sync(srcsh, dstsh, sdir, ddir, "")
	defer println("Finished processing")
	end := time.Now()
	fmt.Println("Total runtime: ", end.Sub(start).Seconds(), " seconds")
}
