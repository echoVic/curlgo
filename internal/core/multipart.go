package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

type multipartBody struct {
	io.Reader
	files []*os.File
}

func (b *multipartBody) Close() error {
	var err error
	for _, f := range b.files {
		err = errors.Join(err, f.Close())
	}
	return err
}

// ValidateMultipartFiles prepares and closes uploads without reading file contents.
func ValidateMultipartFiles(parts []FormPart) error {
	body, _, err := openMultipart(parts, "curlgo-validation")
	if err != nil {
		return err
	}
	return body.Close()
}

// Files remain readers rather than byte slices. Only multipart framing and text
// fields are buffered, so upload memory does not grow with file size.
func openMultipart(parts []FormPart, boundary string) (body *multipartBody, size int64, err error) {
	if err = validateFormParts(parts); err != nil {
		return nil, 0, err
	}
	body = &multipartBody{}
	defer func(opened *multipartBody) {
		if err != nil {
			opened.Close()
		}
	}(body)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err = writer.SetBoundary(boundary); err != nil {
		return nil, 0, err
	}
	var readers []io.Reader
	flush := func() {
		text := buffer.String()
		readers = append(readers, strings.NewReader(text))
		size += int64(len(text))
		buffer.Reset()
	}
	for _, part := range parts {
		if part.Kind == FormText {
			if err = writer.WriteField(part.Name, part.Value); err != nil {
				return nil, 0, err
			}
			flush()
			continue
		}
		filename := filepath.Base(part.FilePath)
		if !validHeaderValue(filename) {
			return nil, 0, fmt.Errorf("文件名不能包含控制字符")
		}
		var info os.FileInfo
		info, err = os.Stat(part.FilePath)
		if err != nil {
			return nil, 0, fmt.Errorf("无法读取文件 %s：%w", filename, err)
		}
		if !info.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("%s 不是普通文件", filename)
		}
		var file *os.File
		file, err = os.Open(part.FilePath)
		if err != nil {
			return nil, 0, fmt.Errorf("无法打开文件 %s：%w", filename, err)
		}
		body.files = append(body.files, file)
		info, err = file.Stat()
		if err != nil {
			return nil, 0, fmt.Errorf("无法读取文件 %s：%w", filename, err)
		}
		if !info.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("%s 不是普通文件", filename)
		}
		if _, err = writer.CreateFormFile(part.Name, filename); err != nil {
			return nil, 0, err
		}
		flush()
		readers = append(readers, io.LimitReader(file, info.Size()))
		size += info.Size()
	}
	if err = writer.Close(); err != nil {
		return nil, 0, err
	}
	flush()
	body.Reader = io.MultiReader(readers...)
	return body, size, nil
}
