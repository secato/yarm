package game

//go:generate go run ../../testdata/pe/gen.go

import (
	"debug/pe"
	"strings"
)

// Inspect opens path as a PE file and returns its architecture and a
// best-guess graphics API based on imported DLLs. It never errors: a
// corrupt, non-PE or unreadable file just yields ArchUnknown/APIUnknown so a
// bad executable doesn't abort a whole directory scan.
//
// Note: debug/pe.File.ImportedLibraries() is a stub in the Go standard
// library (it always returns nil, nil) — see its doc comment. We use
// ImportedSymbols() instead, which actually parses the import directory,
// and derive DLL names from its "func:dll" entries.
//
// debug/pe's own package doc warns that "parsing malformed files may ...
// cause panics", and that isn't hypothetical: a game with an import
// directory pointing outside its section panicked ImportedSymbols with a
// slice-bounds-out-of-range on Go's toolchain up to 1.25.x (fixed in
// 1.26 — see go.mod). Real-world executables are exactly the untrusted,
// arbitrarily-malformed input that warning is about, so the recover below
// stays even after the toolchain bump: one unusual .exe must never take
// down the whole games scan.
func Inspect(path string) (arch Arch, api API) {
	arch, api = ArchUnknown, APIUnknown

	defer func() {
		if recover() != nil {
			arch, api = ArchUnknown, APIUnknown
		}
	}()

	f, err := pe.Open(path)
	if err != nil {
		return ArchUnknown, APIUnknown
	}
	defer func() { _ = f.Close() }()

	arch = archOf(f.Machine)

	symbols, err := f.ImportedSymbols()
	if err != nil {
		return arch, APIUnknown
	}

	return arch, apiOf(importedDLLs(symbols))
}

func archOf(machine uint16) Arch {
	switch machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return ArchX64
	case pe.IMAGE_FILE_MACHINE_I386:
		return ArchX86
	default:
		return ArchUnknown
	}
}

// importedDLLs extracts the lowercased set of DLL names from
// (*pe.File).ImportedSymbols() output, which is formatted "funcName:dllName".
func importedDLLs(symbols []string) map[string]bool {
	dlls := make(map[string]bool)
	for _, sym := range symbols {
		_, dll, ok := strings.Cut(sym, ":")
		if !ok {
			continue
		}
		dlls[strings.ToLower(dll)] = true
	}
	return dlls
}

// apiOf guesses a graphics API from a set of imported DLL names. The
// order matters: a game importing both dxgi and d3d9 is a D3D10+ game
// carrying a legacy dependency, not a D3D9 one.
func apiOf(dlls map[string]bool) API {
	hasPrefix := func(prefix string) bool {
		for dll := range dlls {
			if strings.HasPrefix(dll, prefix) {
				return true
			}
		}
		return false
	}

	switch {
	case dlls["d3d12.dll"]:
		return APID3D12
	case dlls["d3d11.dll"]:
		return APID3D11
	case hasPrefix("d3d10"):
		return APID3D10
	case dlls["dxgi.dll"]:
		return APIDXGI
	case dlls["d3d9.dll"]:
		return APID3D9
	case dlls["d3d8.dll"]:
		return APID3D8
	case dlls["opengl32.dll"]:
		return APIOpenGL
	case dlls["vulkan-1.dll"]:
		return APIVulkan
	default:
		return APIUnknown
	}
}
