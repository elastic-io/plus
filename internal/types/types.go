package types

import (
	"encoding/xml"
	"io"
	"plus/internal/utils"
)

//go:generate easyjson -all types.go
type RepoTable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"` // 新增路径字段，支持多层目录
}

//go:generate easyjson -all types.go
type BatchUploadRequest struct {
	Repository  string `json:"repository"`
	AutoRefresh bool   `json:"auto_refresh"`
}

//go:generate easyjson -all types.go
type BatchUploadResponse struct {
	Status  string              `json:"status"`
	Total   int                 `json:"total"`
	Success int                 `json:"success"`
	Failed  int                 `json:"failed"`
	Results []BatchUploadResult `json:"results"`
}

func (r *BatchUploadResponse) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type BatchUploadResult struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

//go:generate easyjson -all types.go
type Status struct {
	Server  string `json:"server"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (r *Status) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type RepoStatus struct {
	Status Status `json:",inline"`
	Repo   string `json:"repo"`
}

func (r *RepoStatus) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type RepoMeta struct {
	Status       Status               `json:",inline"`
	Repositories []string             `json:"repositories"`
	Tree         map[string]*TreeNode `json:"tree"`
	Count        int                  `json:"count"`
}

func (r *RepoMeta) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type TreeNode struct {
	Type     string               `json:"type"`               // "repo" 或 "directory"
	Path     string               `json:"path,omitempty"`     // 仅对 repo 类型有效
	Children map[string]*TreeNode `json:"children,omitempty"` // 仅对 directory 类型有效
}

//go:generate easyjson -all types.go
type PackageInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Release  string `json:"release"`
	Arch     string `json:"arch"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

//go:generate easyjson -all types.go
type RepoInfo struct {
	Status       Status        `json:",inline"`
	Name         string        `json:"name"`
	PackageCount int           `json:"package_count"`
	RPMCount     int           `json:"rpm_count"`
	DEBCount     int           `json:"deb_count"`
	TotalSize    int64         `json:"total_size"`
	Packages     []PackageInfo `json:"packages"`
}

func (r *RepoInfo) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type Metrics struct {
	Requests    Requests    `json:"requests"`
	Performance Performance `json:"performance"`
	Memory      Memory      `json:"memory"`
}

func (r *Metrics) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type Performance struct {
	ResponseTimeMs int64 `json:"response_time_ms"`
	Goroutines     int   `json:"goroutines"`
}

//go:generate easyjson -all types.go
type Requests struct {
	Total     int64 `json:"total"`
	Uploads   int64 `json:"uploads"`
	Downloads int64 `json:"downloads"`
	Errors    int64 `json:"errors"`
	Active    int64 `json:"active"`
}

//go:generate easyjson -all types.go
type Memory struct {
	AllocMB      uint64 `json:"alloc_mb"`
	TotalAllocMB uint64 `json:"total_alloc_mb"`
	SysMB        uint64 `json:"sys_mb"`
	GCCycles     uint32 `json:"gc_cycles"`
}

//go:generate easyjson -all types.go
type ReadyCheck struct {
	Status Status `json:"status"`
	Checks Checks `json:"checks"`
}

func (r *ReadyCheck) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(r, w) }

//go:generate easyjson -all types.go
type PackageChecksum struct {
	Status   Status `json:"status"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Repo     string `json:"repo"`
}

func (pc *PackageChecksum) WriteTo(w io.Writer) (int64, error) { return utils.WriteTo(pc, w) }

//go:generate easyjson -all types.go
type Checks struct {
	Storage string
}

type Metadata struct {
	XMLName  xml.Name  `xml:"metadata"`
	Packages []Package `xml:"package"`
}

type Package struct {
	Name     string   `xml:"name"`
	Arch     string   `xml:"arch"`
	Version  Version  `xml:"version"`
	Checksum Checksum `xml:"checksum"`
	Location Location `xml:"location"`
}

type Version struct {
	Epoch string `xml:"epoch,attr"`
	Ver   string `xml:"ver,attr"`
	Rel   string `xml:"rel,attr"`
}

type Checksum struct {
	Type  string `xml:"type,attr"`
	Pkgid string `xml:"pkgid,attr"`
	Value string `xml:",chardata"`
}

type Location struct {
	Href string `xml:"href,attr"`
}
