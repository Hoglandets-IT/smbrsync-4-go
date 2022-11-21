package smbrsync

import (
	"io"
	"io/fs"
	"os"

	"regexp"
	"strings"

	"github.com/hirochachacha/go-smb2"
)

type SmbRsyncResult struct {
	Copied   []string
	Skipped  []string
	Excluded []string
	Mismatch []string
	Deleted  []string
}

type SmbRsyncShare struct {
	Share    *smb2.Share
	BasePath string
}

type SmbRsync struct {
	src     *SmbRsyncShare
	dst     *SmbRsyncShare
	filters []*regexp.Regexp
	res     SmbRsyncResult
}

func New(src *SmbRsyncShare, dst *SmbRsyncShare, filters []string) (*SmbRsync, error) {

	// normalize paths
	src.BasePath = normPath(src.BasePath)
	dst.BasePath = normPath(dst.BasePath)

	// compile filters
	var _filters []*regexp.Regexp
	for _, filter := range filters {
		r, err := regexp.Compile(filter)
		if err != nil {
			return nil, err
		}

		_filters = append(_filters, r)
	}

	return &SmbRsync{
		src: src,
		dst: dst,

		filters: _filters,

		res: SmbRsyncResult{
			Copied:   []string{},
			Skipped:  []string{},
			Excluded: []string{},
			Mismatch: []string{},
			Deleted:  []string{},
		},
	}, nil
}

// Windows path separator
const PathSeparator = '\\'

func (sync *SmbRsync) logCopied(item string) {
	sync.res.Copied = append(sync.res.Copied, item)
}

func (sync *SmbRsync) logSkipped(item string) {
	sync.res.Skipped = append(sync.res.Skipped, item)
}

func (sync *SmbRsync) logExcluded(item string) {
	// this one needs some checks for duplication
	if !sliceContains(sync.res.Excluded, item) {
		sync.res.Excluded = append(sync.res.Excluded, item)
	}
}

func (sync *SmbRsync) logMismatch(item string) {
	sync.res.Mismatch = append(sync.res.Mismatch, item)
}

func (sync *SmbRsync) logDeleted(item string) {
	sync.res.Deleted = append(sync.res.Deleted, item)
}

// Returns a normalized path,
// Meaning:
//   - All separators are replaced with 'PathSeparator'
//   - Any leading or trailing separators are removed
//   - Current directory prefix or full path is removed.
func normPath(path string) string {
	path = strings.Replace(path, `/`, `\`, -1)
	path = strings.Trim(path, `\`)
	for strings.HasPrefix(path, `.\`) {
		path = path[2:]
	}
	if path == "." {
		return ""
	}
	return path
}

// checks if a slice of strings contains a given value
func sliceContains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

// Returns a normalized path constructed by 'elem' separated by 'PathSeparator'
func joinPath(elem ...string) string {
	return normPath(strings.Join(elem, string(PathSeparator)))
}

// Checks if two files differ.
// For now this is done only via the following methods
//   - Second file does not exist
//   - Modification time differs.
//   - File size differs.
func fileDiff(fi1 fs.FileInfo, fi2 fs.FileInfo) bool {
	return fi2 == nil || fi1.ModTime() != fi2.ModTime() || fi1.Size() != fi2.Size()
}

// Wrapper for smb2.Share.ReadDir() with filtering.
func (sync *SmbRsync) filteredDir(share *SmbRsyncShare, subPath string) ([]fs.FileInfo, error) {

	// get the contents directory
	items, err := share.Share.ReadDir(joinPath(share.BasePath, subPath))
	if err != nil {
		return nil, err
	}

	// nothing to do..
	if len(items) == 0 || len(sync.filters) == 0 {
		return items, nil
	}

	// loop over each filter
	var filtered []fs.FileInfo
	for _, item := range items {
		matched := false
		for _, filter := range sync.filters {
			if filter.MatchString(joinPath(subPath, item.Name())) {
				matched = true
				break
			}
		}

		if matched {
			sync.logExcluded(joinPath(subPath, item.Name()))
		} else {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

// Performs a recursive sync of two folders on 2 shares
//
// the contents of the two folders are listed.
// for each source item a check is performed
//
// File:
//   - recursively delete destination if destination is a directory
//   - check if destination file exists
//     -- if it does not, create it and write source data to it
//     -- if it does, compare source and destination
//     -- if they differ, overwrite destination data with data from source
//
// Directory:
//   - delete destination if destination is a file
//   - create destination directory if it does not exist
//   - call 'recursiveSync' with the sub/folder appended to the respective path of each
//
// Then remove any files and directories from destination that does not exist in source.
func (sync *SmbRsync) recursiveSync(subPath string) error {

	lss, err := sync.filteredDir(sync.src, subPath)
	if err != nil {
		return err
	}

	// contents of destination as map of item names
	tmp, err := sync.filteredDir(sync.dst, subPath)
	if err != nil {
		return err
	}

	lsd := map[string]fs.FileInfo{}
	for _, item := range tmp {
		lsd[item.Name()] = item
	}

	for _, item := range lss {
		srcItemPath := joinPath(sync.src.BasePath, subPath, item.Name())
		dstItemPath := joinPath(sync.dst.BasePath, subPath, item.Name())

		if item.Mode().IsRegular() {
			// source is regular file

			// check if exists in destination
			_, found := lsd[item.Name()]

			// check if regular file, only checks if found
			isFile := found && lsd[item.Name()].Mode().IsRegular()

			// check diff, only checks diff if found
			diff := found && isFile && fileDiff(item, lsd[item.Name()])

			// log as copied file
			if !found {
				sync.logCopied(joinPath(subPath, item.Name()))
			}

			// log as mismatch and remove destination
			if found && !isFile {
				err := sync.dst.Share.RemoveAll(dstItemPath)
				if err != nil {
					return err
				}

				sync.logMismatch(joinPath(subPath, item.Name()))
			}

			// log as mismatch
			if found && isFile && diff {
				sync.logMismatch(joinPath(subPath, item.Name()))
			}

			if !found || diff {
				// destination does not exist or file mismatch
				// create and/or write source data to destination
				src, err := sync.src.Share.Open(srcItemPath)
				if err != nil {
					return err
				}
				defer src.Close()

				dst, err := sync.src.Share.OpenFile(dstItemPath, os.O_CREATE|os.O_TRUNC, item.Mode().Perm())
				if err != nil {
					return err
				}
				defer dst.Close()

				buf := make([]byte, 1024*1024*2)
				for {
					n, err := src.Read(buf)
					if err != nil && err != io.EOF {
						return err
					}

					if err == io.EOF || n == 0 {
						break
					}

					if _, err := dst.Write(buf[:n]); err != nil {
						return err
					}
				}

				err = sync.dst.Share.Chtimes(dstItemPath, item.ModTime(), item.ModTime())
				if err != nil {
					return err
				}
			}

			// log as skip
			if found && isFile && !diff {
				sync.logSkipped(joinPath(subPath, item.Name()))
			}
		} else if item.Mode().IsDir() {
			// source is directory
			if _, found := lsd[item.Name()]; !found {
				// not found, add directory, log as copied
				err := sync.dst.Share.Mkdir(dstItemPath, item.Mode())
				if err != nil {
					return err
				}

				sync.logCopied(joinPath(subPath, item.Name()))
			} else if !lsd[item.Name()].Mode().IsDir() {
				// not a directory, make it one, log as mismatch
				err := sync.dst.Share.Remove(dstItemPath)
				if err != nil {
					return err
				}

				err = sync.dst.Share.Mkdir(dstItemPath, item.Mode())
				if err != nil {
					return err
				}

				sync.logMismatch(joinPath(subPath, item.Name()))
			} else {
				sync.logSkipped(joinPath(subPath, item.Name()))
			}

			err = sync.recursiveSync(joinPath(subPath, item.Name()))
			if err != nil {
				return err
			}
		}
		// remove item from destination item map
		delete(lsd, item.Name())
	}

	// anything left in the destination item map should be removed
	// log as deleted
	for _, item := range lsd {
		err := sync.dst.Share.RemoveAll(joinPath(sync.dst.BasePath, subPath, item.Name()))
		if err != nil {
			return err
		}

		sync.logDeleted(joinPath(subPath, item.Name()))
	}

	return nil
}

// Performs a sync of two folders on 2 shares
func (sync *SmbRsync) Sync() (*SmbRsyncResult, error) {

	// perform sync
	err := sync.recursiveSync("")
	if err != nil {
		return nil, err
	}

	return &sync.res, nil
}
