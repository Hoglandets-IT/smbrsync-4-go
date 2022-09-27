package smbrsync

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/hirochachacha/go-smb2"
)

const PathSeparator = '\\'

func normPath(path string) string {
	path = strings.Replace(path, `/`, `\`, -1)
	path = strings.Trim(path, `/`)
	for strings.HasPrefix(path, `.\`) {
		path = path[2:]
	}
	if path == "." {
		return ""
	}
	return path
}

func joinPath(elem ...string) string {
	return normPath(strings.Join(elem, string(PathSeparator)))
}

func fileDiff(fi1 fs.FileInfo, fi2 fs.FileInfo) bool {
	return fi2 == nil || fi1.ModTime() != fi2.ModTime() || fi1.Size() != fi2.Size()
}

func recursiveSync(srcShare *smb2.Share, dstShare *smb2.Share, srcPath string, dstPath string) {

	lss, err := srcShare.ReadDir(srcPath)
	if err != nil {
		panic(err)
	}

	// contents of destination as map of item names
	tmp, err := dstShare.ReadDir(dstPath)
	if err != nil {
		panic(err)
	}
	lsd := make(map[string]fs.FileInfo, 0)
	for _, item := range tmp {
		lsd[item.Name()] = item
	}

	for _, item := range lss {
		srcItemPath := joinPath(srcPath, item.Name())
		dstItemPath := joinPath(dstPath, item.Name())

		if item.Mode().IsRegular() {
			// source is regular file
			if _, found := lsd[item.Name()]; found && !lsd[item.Name()].Mode().IsRegular() {
				// destination exists but is not a file
				err := dstShare.RemoveAll(dstItemPath)
				if err != nil {
					panic(err)
				}
				// remove from destination item map for next step of check
				delete(lsd, item.Name())
			}

			if _, found := lsd[item.Name()]; !found || fileDiff(item, lsd[item.Name()]) {
				// destination does not exist of file differ
				fmt.Println("Files differ ", srcItemPath)
				srcont, err := srcShare.ReadFile(srcItemPath)
				if err != nil {
					panic(err)
				}

				dstShare.WriteFile(dstItemPath, srcont, item.Mode())
				dstShare.Chtimes(dstItemPath, item.ModTime(), item.ModTime())
			}
		} else if item.Mode().IsDir() {
			// source is directory
			if _, found := lsd[item.Name()]; !found {
				// not found, add directory
				err := dstShare.Mkdir(dstItemPath, item.Mode())
				if err != nil {
					panic(err)
				}
			} else if !lsd[item.Name()].Mode().IsDir() {
				// not a directory, make it one
				err := dstShare.Remove(dstItemPath)
				if err != nil {
					panic(err)
				}

				err = dstShare.Mkdir(dstItemPath, item.Mode())
				if err != nil {
					panic(err)
				}
			}

			recursiveSync(srcShare, dstShare, joinPath(srcPath, item.Name()), joinPath(dstPath, item.Name()))
		}
		// remove item from destination item map
		delete(lsd, item.Name())
	}

	// anything left in the destination item map should be removed
	for _, item := range lsd {
		dstItemPath := joinPath(dstPath, item.Name())
		err := dstShare.RemoveAll(dstItemPath)
		if err != nil {
			panic(err)
		}
	}
}

func Sync(srcShare *smb2.Share, dstShare *smb2.Share, srcPath string, dstPath string) {
	srcPath = normPath(srcPath)
	dstPath = normPath(dstPath)

	recursiveSync(srcShare, dstShare, srcPath, dstPath)
}
