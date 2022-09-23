package main

import (
	"crypto/md5"
	"fmt"
	"io/fs"
	"net"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
)

func contains(s []fs.FileInfo, val string) bool {
	for _, v := range s {
		if v.Name() == val {
			return true
		}
	}

	return false
}

func get_checksum(con *smb2.Share, path string) string {
	f, err := con.ReadFile(path)
	if err != nil {
		panic(err)
	}

	return fmt.Sprintf("%x", md5.Sum(f))
}

func build_path(strs ...string) string {
	restr := ""
	for _, s := range strs {
		if strings.HasSuffix(restr, "/") && strings.HasPrefix(s, "/") {
			s = s[1:]
		} else if !strings.HasSuffix(restr, "/") && !strings.HasPrefix(s, "/") {
			s = "/" + s
		}
		restr += s
	}
	return restr[1:]
}

func files_differ(t1 fs.FileInfo, t2 fs.FileInfo) bool {
	return t2 == nil || t1.ModTime() != t2.ModTime() || t1.Size() != t2.Size()
}

func reco_sync(srconn *smb2.Share, dsconn *smb2.Share, srpath string, dspath string, subpath string) {
	scur_path := build_path(srpath, subpath)
	dcur_path := build_path(dspath, subpath)

	lss, err := srconn.ReadDir(scur_path)
	if err != nil {
		panic(err)
	}

	// contents of destination as map of item names
	tmp, err := dsconn.ReadDir(dcur_path)
	if err != nil {
		panic(err)
	}
	lsd := make(map[string]fs.FileInfo, 0)
	for _, item := range tmp {
		lsd[item.Name()] = item
	}

	for _, item := range lss {
		sItemPath := build_path(scur_path, item.Name())
		dItemPath := build_path(dcur_path, item.Name())
		if item.Mode().IsRegular() {
			dfile, err := dsconn.Stat(dItemPath)
			if err != nil && !strings.Contains(err.Error(), "does not exist") {
				panic(err)
			}
			if files_differ(item, dfile) {
				fmt.Println("Files differ ", sItemPath)
				srcont, err := srconn.ReadFile(sItemPath)
				if err != nil {
					panic(err)
				}
				dsconn.WriteFile(dItemPath, srcont, item.Mode())
				dsconn.Chtimes(dItemPath, item.ModTime(), item.ModTime())
			}
		} else if item.Mode().IsDir() {
			// directory
			if _, found := lsd[item.Name()]; !found {
				// not found, add file
				err := dsconn.Mkdir(dItemPath, item.Mode())
				if err != nil {
					panic(err)
				}
			} else if !lsd[item.Name()].IsDir() {
				// not a directory, make it one
				err := dsconn.Remove(dItemPath)
				if err != nil {
					panic(err)
				}

				err = dsconn.Mkdir(dItemPath, item.Mode())
				if err != nil {
					panic(err)
				}
			}

			reco_sync(srconn, dsconn, srpath, dspath, build_path(subpath, item.Name()))
		}
		// remove item from destination item map
		delete(lsd, item.Name())
	}

	// anything left in the destination item map should be removed
	for _, item := range lsd {
		dItemPath := build_path(dcur_path, item.Name())
		err := dsconn.Remove(dItemPath)
		if err != nil {
			panic(err)
		}
	}
}

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
