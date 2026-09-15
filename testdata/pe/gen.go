//go:build ignore

// Command gen writes the tiny synthetic PE files under testdata/pe used by
// internal/game's PE-inspection tests. Each is the smallest image
// debug/pe.Open (and (*pe.File).ImportedSymbols) can parse: a DOS stub, COFF
// file header, a minimal optional header with two data directories (export
// unused, import populated), and — when there are imports — one section
// holding the import directory table, per-DLL import lookup tables,
// hint/name entries and DLL name strings.
//
// Run via `go generate ./...` from internal/game. Regenerating changes these
// files' bytes but not their names or the DLLs/architectures they encode;
// commit the result.
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const (
	machineI386  = 0x14c
	machineAMD64 = 0x8664
)

type importSpec struct {
	dll string
	fn  string
}

type fixture struct {
	name    string
	machine uint16
	imports []importSpec
	// corruptThunk overwrites the first import descriptor's
	// OriginalFirstThunk with an RVA far outside the section, reproducing
	// the malformed import table that panicked (*pe.File).ImportedSymbols
	// with a slice-bounds-out-of-range on Go toolchains up to 1.25.x.
	corruptThunk bool
}

var fixtures = []fixture{
	{"x64_dxgi.bin", machineAMD64, []importSpec{{"dxgi.dll", "CreateDXGIFactory"}}, false},
	{"x64_d3d11.bin", machineAMD64, []importSpec{{"d3d11.dll", "D3D11CreateDevice"}, {"dxgi.dll", "CreateDXGIFactory"}}, false},
	{"x64_d3d12.bin", machineAMD64, []importSpec{{"d3d12.dll", "D3D12CreateDevice"}}, false},
	{"x86_d3d10.bin", machineI386, []importSpec{{"d3d10_1.dll", "D3D10CreateDevice1"}}, false},
	{"x86_d3d9.bin", machineI386, []importSpec{{"d3d9.dll", "Direct3DCreate9"}}, false},
	{"x86_d3d8.bin", machineI386, []importSpec{{"d3d8.dll", "Direct3DCreate8"}}, false},
	{"x86_opengl.bin", machineI386, []importSpec{{"opengl32.dll", "wglCreateContext"}}, false},
	{"x64_vulkan.bin", machineAMD64, []importSpec{{"vulkan-1.dll", "vkCreateInstance"}}, false},
	{"x64_unknown.bin", machineAMD64, []importSpec{{"kernel32.dll", "GetModuleHandleA"}}, false},
	{"x64_no_imports.bin", machineAMD64, nil, false},
	{"x64_malformed_import.bin", machineAMD64, []importSpec{{"dxgi.dll", "CreateDXGIFactory"}}, true},
}

func main() {
	outDir := "testdata/pe"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}

	for _, fx := range fixtures {
		data := buildPE(fx.machine, fx.imports)
		if fx.corruptThunk {
			corruptFirstThunk(data)
		}
		path := filepath.Join(outDir, fx.name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			panic(err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(data))
	}
}

const (
	dosHeaderSize    = 0x40
	peSignatureSize  = 4
	fileHeaderSize   = 20
	sectionHeaderSz  = 40
	numDataDirs      = 2
	dataDirEntrySize = 8
	sectionRVA       = 0x1000
	oh32MinSz        = 96  // sizeof(OptionalHeader32) - sizeof(DataDirectory array)
	oh64MinSz        = 112 // sizeof(OptionalHeader64) - sizeof(DataDirectory array)
)

func buildPE(machine uint16, imports []importSpec) []byte {
	is64 := machine == machineAMD64
	thunkSize := 4
	if is64 {
		thunkSize = 8
	}

	optHeaderSize := oh32MinSz + numDataDirs*dataDirEntrySize
	if is64 {
		optHeaderSize = oh64MinSz + numDataDirs*dataDirEntrySize
	}

	numSections := 0
	if len(imports) > 0 {
		numSections = 1
	}

	headersSize := dosHeaderSize + peSignatureSize + fileHeaderSize + optHeaderSize + numSections*sectionHeaderSz
	sectionDataOffset := headersSize

	var sectionData []byte
	var importDirRVA, importDirSize uint32
	if len(imports) > 0 {
		sectionData, importDirRVA, importDirSize = buildImportSection(sectionRVA, thunkSize, imports)
	}

	buf := make([]byte, headersSize+len(sectionData))

	// DOS header: "MZ" + e_lfanew at 0x3C pointing right after the stub.
	buf[0], buf[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(buf[0x3C:], dosHeaderSize)

	off := dosHeaderSize
	copy(buf[off:], []byte("PE\x00\x00"))
	off += peSignatureSize

	// COFF file header.
	binary.LittleEndian.PutUint16(buf[off:], machine)
	binary.LittleEndian.PutUint16(buf[off+2:], uint16(numSections))
	// TimeDateStamp, PointerToSymbolTable, NumberOfSymbols left zero.
	binary.LittleEndian.PutUint16(buf[off+16:], uint16(optHeaderSize))
	binary.LittleEndian.PutUint16(buf[off+18:], 0x0002) // IMAGE_FILE_EXECUTABLE_IMAGE
	off += fileHeaderSize

	// Optional header: only Magic and the tail (NumberOfRvaAndSizes +
	// DataDirectory) matter for our test; everything else stays zero.
	ohStart := off
	if is64 {
		binary.LittleEndian.PutUint16(buf[ohStart:], 0x20b) // PE32+
	} else {
		binary.LittleEndian.PutUint16(buf[ohStart:], 0x10b) // PE32
	}
	off += optHeaderSize

	ddStart := off - numDataDirs*dataDirEntrySize
	numRvaOff := ddStart - 4
	binary.LittleEndian.PutUint32(buf[numRvaOff:], numDataDirs)
	if importDirSize > 0 {
		binary.LittleEndian.PutUint32(buf[ddStart+dataDirEntrySize:], importDirRVA)
		binary.LittleEndian.PutUint32(buf[ddStart+dataDirEntrySize+4:], importDirSize)
	}

	if numSections > 0 {
		copy(buf[off:], append([]byte(".idata"), 0, 0))
		binary.LittleEndian.PutUint32(buf[off+8:], uint32(len(sectionData)))   // VirtualSize
		binary.LittleEndian.PutUint32(buf[off+12:], sectionRVA)                // VirtualAddress
		binary.LittleEndian.PutUint32(buf[off+16:], uint32(len(sectionData)))  // SizeOfRawData
		binary.LittleEndian.PutUint32(buf[off+20:], uint32(sectionDataOffset)) // PointerToRawData
		binary.LittleEndian.PutUint32(buf[off+36:], 0x40000040)                // INITIALIZED_DATA | MEM_READ

		copy(buf[sectionDataOffset:], sectionData)
	}

	return buf
}

// corruptFirstThunk overwrites the OriginalFirstThunk field of the first
// import descriptor in data's .idata section with an RVA far past the end
// of the section, in place.
func corruptFirstThunk(data []byte) {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	sec := f.Section(".idata")
	if sec == nil {
		panic("corruptFirstThunk: no .idata section")
	}
	binary.LittleEndian.PutUint32(data[sec.Offset:], sec.VirtualAddress+0x7fff0000)
}

// buildImportSection lays out the import directory table, per-DLL import
// lookup tables, hint/name entries and DLL name strings in one contiguous
// blob addressed relative to baseRVA. It returns the blob and the RVA/size
// of the import directory table (for DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT]).
func buildImportSection(baseRVA uint32, thunkSize int, imports []importSpec) (data []byte, dirRVA, dirSize uint32) {
	const descSize = 20
	dirRVA = baseRVA
	dirSize = uint32((len(imports) + 1) * descSize) // +1 null-descriptor terminator

	type layout struct {
		intRVA, hintNameRVA, nameRVA       uint32
		intBytes, hintNameBytes, nameBytes []byte
	}

	cursor := dirSize
	layouts := make([]layout, len(imports))
	for i, imp := range imports {
		var l layout

		l.intRVA = baseRVA + cursor
		l.intBytes = make([]byte, 2*thunkSize) // one real entry + zero terminator
		cursor += uint32(len(l.intBytes))

		l.hintNameRVA = baseRVA + cursor
		l.hintNameBytes = append([]byte{0, 0}, append([]byte(imp.fn), 0)...)
		if len(l.hintNameBytes)%2 != 0 {
			l.hintNameBytes = append(l.hintNameBytes, 0)
		}
		cursor += uint32(len(l.hintNameBytes))

		l.nameRVA = baseRVA + cursor
		l.nameBytes = append([]byte(imp.dll), 0)
		cursor += uint32(len(l.nameBytes))

		if thunkSize == 8 {
			binary.LittleEndian.PutUint64(l.intBytes[0:8], uint64(l.hintNameRVA))
		} else {
			binary.LittleEndian.PutUint32(l.intBytes[0:4], l.hintNameRVA)
		}
		layouts[i] = l
	}

	data = make([]byte, cursor)
	for i := range imports {
		descOff := i * descSize
		binary.LittleEndian.PutUint32(data[descOff:], layouts[i].intRVA)     // OriginalFirstThunk
		binary.LittleEndian.PutUint32(data[descOff+12:], layouts[i].nameRVA) // Name
		// TimeDateStamp, ForwarderChain, FirstThunk left zero.
	}
	// The null terminator descriptor is already all-zero from make().

	for _, l := range layouts {
		copy(data[l.intRVA-baseRVA:], l.intBytes)
		copy(data[l.hintNameRVA-baseRVA:], l.hintNameBytes)
		copy(data[l.nameRVA-baseRVA:], l.nameBytes)
	}

	return data, dirRVA, dirSize
}
