package lfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

type cleanedAsset struct {
	Filename string
	*Pointer
}

type CleanedPointerError struct {
	Pointer *Pointer
	Bytes   []byte
}

func (e *CleanedPointerError) Error() string {
	return "Cannot clean a Git LFS pointer.  Skipping."
}

func PointerClean(reader io.Reader, fileName string, fileSize int64, cb CopyCallback) (*cleanedAsset, error) {
	extensions, err := SortExtensions(Config.Extensions())
	if err != nil {
		return nil, err
	}

	oid, size, tmp, err := copyToTemp(reader, fileSize, cb)
	if err != nil {
		return nil, err
	}

	var exts []PointerExtension
	if len(extensions) > 0 {
		if tmp, err = os.Open(tmp.Name()); err != nil {
			return nil, err
		}

		if oid, tmp, exts, err = pipeExtensions(tmp, oid, fileName, extensions); err != nil {
			return nil, err
		}

		var stat os.FileInfo
		if stat, err = os.Stat(tmp.Name()); err != nil {
			return nil, err
		}
		size = stat.Size()
	}

	pointer := NewPointer(oid, size, exts)
	return &cleanedAsset{tmp.Name(), pointer}, err
}

func copyToTemp(reader io.Reader, fileSize int64, cb CopyCallback) (oid string, size int64, tmp *os.File, err error) {
	tmp, err = TempFile("")
	if err != nil {
		return
	}

	defer tmp.Close()

	oidHash := sha256.New()
	writer := io.MultiWriter(oidHash, tmp)

	if fileSize == 0 {
		cb = nil
	}

	by, ptr, err := DecodeFrom(reader)
	if err == nil && len(by) < 512 {
		err = &CleanedPointerError{ptr, by}
		return
	}

	multi := io.MultiReader(bytes.NewReader(by), reader)
	size, err = CopyWithCallback(writer, multi, fileSize, cb)

	if err != nil {
		return
	}

	oid = hex.EncodeToString(oidHash.Sum(nil))
	return
}

func (a *cleanedAsset) Teardown() error {
	return os.Remove(a.Filename)
}
