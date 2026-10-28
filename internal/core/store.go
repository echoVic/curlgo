package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxWorkspace = 16 << 20

type Store struct {
	Dir string
	mu  sync.Mutex
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}
func decodeWorkspace(data []byte) (Workspace, error) {
	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return w, err
	}
	if w.Version != 1 {
		return w, fmt.Errorf("不支持的存储版本 %d", w.Version)
	}
	if w.Settings.Environments == nil {
		w.Settings.Environments = map[string]map[string]string{}
	}
	return w, nil
}
func readWorkspace(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxWorkspace+1))
	if len(b) > maxWorkspace {
		return nil, fmt.Errorf("本地文件超过 16 MiB")
	}
	return b, err
}
func (s *Store) Load() (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Dir, "workspace.json")
	data, err := readWorkspace(path)
	if errors.Is(err, os.ErrNotExist) {
		backup, be := readWorkspace(path + ".bak")
		if errors.Is(be, os.ErrNotExist) {
			return NewWorkspace(), nil
		}
		if be != nil {
			return Workspace{}, be
		}
		w, de := decodeWorkspace(backup)
		if de != nil {
			return Workspace{}, fmt.Errorf("备份损坏：%w", de)
		}
		return w, fmt.Errorf("主文件缺失，已从备份恢复")
	}
	if err == nil {
		var w Workspace
		w, err = decodeWorkspace(data)
		if err == nil {
			return w, nil
		}
	}
	backup, be := readWorkspace(path + ".bak")
	if be == nil {
		var w Workspace
		w, be = decodeWorkspace(backup)
		if be == nil {
			if e := os.Rename(path, path+".corrupt-"+time.Now().Format("20060102-150405.000000000")); e != nil {
				return Workspace{}, fmt.Errorf("保留损坏文件失败：%w", e)
			}
			return w, fmt.Errorf("已从备份恢复，损坏原件已保留")
		}
	}
	return Workspace{}, fmt.Errorf("本地数据无法读取，已保留原文件：%w", err)
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".qingzhou-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	// Directory fsync makes the rename durable on Unix; some filesystems reject it.
	if d, e := os.Open(filepath.Dir(path)); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func (s *Store) Save(w Workspace) error {
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxWorkspace {
		return fmt.Errorf("本地数据超过 16 MiB，请清理历史或请求")
	}
	if _, err = decodeWorkspace(data); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Dir, "workspace.json")
	old, err := readWorkspace(path)
	if err == nil {
		previous, e := decodeWorkspace(old)
		if e != nil {
			return fmt.Errorf("原文件损坏，未覆盖：%w", e)
		}
		// Recovery can restore prior requests, but must not revive a deleted
		// login session or roll a server's updated cookies back.
		previous.Cookies = w.Cookies
		old, err = json.MarshalIndent(previous, "", "  ")
		if err == nil && len(old) > maxWorkspace {
			// A backup without cookies is still recoverable; an oversized one is not.
			previous.Cookies = nil
			old, err = json.MarshalIndent(previous, "", "  ")
		}
		if err != nil {
			return err
		}
		if len(old) > maxWorkspace {
			return fmt.Errorf("备份超过 16 MiB，请清理历史或请求")
		}
		if err = atomicWrite(path+".bak", old); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, data)
}
