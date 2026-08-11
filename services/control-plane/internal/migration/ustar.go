package migration

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	tarBlockSize      = 512
	maxRuntimeTarSize = 64 << 20
	maxRuntimeFiles   = 8192
)

type tarMember struct {
	Path string
	Data []byte
}

func parseDeterministicUSTAR(data []byte) ([]tarMember, error) {
	if len(data) == 0 || len(data) > maxRuntimeTarSize || len(data)%tarBlockSize != 0 {
		return nil, fail(CodeInvalidArchive, "ustar", "archive size is empty, oversized, or not block aligned", nil)
	}
	members := make([]tarMember, 0)
	offset := 0
	previousPath := ""
	for {
		if offset+2*tarBlockSize > len(data) {
			return nil, fail(CodeInvalidArchive, "ustar", "archive lacks exactly two terminal zero blocks", nil)
		}
		header := data[offset : offset+tarBlockSize]
		if isZeroBlock(header) {
			if !isZeroBlock(data[offset+tarBlockSize:offset+2*tarBlockSize]) || offset+2*tarBlockSize != len(data) {
				return nil, fail(CodeInvalidArchive, "ustar", "terminal blocks are malformed or followed by trailing data", nil)
			}
			return members, nil
		}
		if len(members) >= maxRuntimeFiles {
			return nil, fail(CodeInvalidArchive, "ustar", "file count limit exceeded", nil)
		}
		member, size, err := parseUSTARHeader(header)
		if err != nil {
			return nil, err
		}
		if previousPath != "" && previousPath >= member.Path {
			return nil, fail(CodeInvalidArchive, member.Path, "members are not in strict ASCII path order", nil)
		}
		offset += tarBlockSize
		padded := (size + tarBlockSize - 1) / tarBlockSize * tarBlockSize
		if size < 0 || offset+padded > len(data) {
			return nil, fail(CodeInvalidArchive, member.Path, "member data exceeds archive", nil)
		}
		member.Data = bytes.Clone(data[offset : offset+size])
		if !isZeroBlock(data[offset+size : offset+padded]) {
			return nil, fail(CodeInvalidArchive, member.Path, "member padding is not zero-filled", nil)
		}
		members = append(members, member)
		previousPath = member.Path
		offset += padded
	}
}

func parseUSTARHeader(header []byte) (tarMember, int, error) {
	if len(header) != tarBlockSize {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "invalid header size", nil)
	}
	if !bytes.Equal(header[257:263], []byte{'u', 's', 't', 'a', 'r', 0}) || !bytes.Equal(header[263:265], []byte{'0', '0'}) {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "GNU/PAX/non-ustar header is forbidden", nil)
	}
	if !bytes.Equal(header[100:108], []byte("0000644\x00")) || !bytes.Equal(header[108:116], []byte("0000000\x00")) || !bytes.Equal(header[116:124], []byte("0000000\x00")) || !bytes.Equal(header[136:148], []byte("00000000000\x00")) {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "mode, uid, gid, or mtime is not canonical", nil)
	}
	if header[156] != '0' || !allZero(header[157:257]) || !allZero(header[265:345]) || !allZero(header[329:337]) || !allZero(header[337:345]) || !allZero(header[500:512]) {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "link, owner, device, extension, or type fields are not canonical", nil)
	}
	checksumText := header[148:156]
	if checksumText[6] != 0 || checksumText[7] != ' ' {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "checksum field encoding is not canonical", nil)
	}
	checksum, err := parseFixedOctal(checksumText[:6], 6)
	if err != nil {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "invalid checksum", err)
	}
	copyHeader := bytes.Clone(header)
	for index := 148; index < 156; index++ {
		copyHeader[index] = ' '
	}
	computed := 0
	for _, value := range copyHeader {
		computed += int(value)
	}
	if checksum != computed || !bytes.Equal(checksumText, []byte(fmt.Sprintf("%06o\x00 ", computed))) {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "header checksum mismatch", nil)
	}
	sizeField := header[124:136]
	if sizeField[11] != 0 {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "size field is not canonical", nil)
	}
	size, err := parseFixedOctal(sizeField[:11], 11)
	if err != nil || !bytes.Equal(sizeField, []byte(fmt.Sprintf("%011o\x00", size))) {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "invalid size field", err)
	}
	name, ok := nulPaddedASCII(header[0:100])
	if !ok || name == "" {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "invalid member name", nil)
	}
	prefix, ok := nulPaddedASCII(header[345:500])
	if !ok {
		return tarMember{}, 0, fail(CodeInvalidArchive, "ustar", "invalid member prefix", nil)
	}
	fullPath := name
	if prefix != "" {
		fullPath = prefix + "/" + name
	}
	if err := validateArtifactPath(fullPath); err != nil {
		return tarMember{}, 0, err
	}
	expectedPrefix, expectedName, splitErr := splitUSTARPath(fullPath)
	if splitErr != nil || expectedPrefix != prefix || expectedName != name {
		return tarMember{}, 0, fail(CodeInvalidArchive, fullPath, "ustar path split is not canonical", splitErr)
	}
	return tarMember{Path: fullPath}, size, nil
}

func splitUSTARPath(value string) (string, string, error) {
	if len(value) <= 100 {
		return "", value, nil
	}
	best := -1
	for index := range value {
		if value[index] == '/' && index <= 155 && len(value)-index-1 <= 100 && len(value)-index-1 > 0 {
			best = index
		}
	}
	if best < 0 {
		return "", "", fail(CodeInvalidArchive, value, "path cannot be represented by ustar", nil)
	}
	return value[:best], value[best+1:], nil
}

func parseFixedOctal(value []byte, width int) (int, error) {
	if len(value) != width {
		return 0, fmt.Errorf("wrong width")
	}
	for _, digit := range value {
		if digit < '0' || digit > '7' {
			return 0, fmt.Errorf("non-octal digit")
		}
	}
	parsed, err := strconv.ParseUint(string(value), 8, 63)
	return int(parsed), err
}

func nulPaddedASCII(value []byte) (string, bool) {
	end := bytes.IndexByte(value, 0)
	if end < 0 {
		end = len(value)
	}
	if end < len(value) && !allZero(value[end:]) {
		return "", false
	}
	text := string(value[:end])
	for _, char := range text {
		if char < 0x21 || char > 0x7e {
			return "", false
		}
	}
	return text, true
}

func allZero(value []byte) bool {
	return strings.Trim(string(value), "\x00") == ""
}

func isZeroBlock(value []byte) bool { return len(value) == 0 || allZero(value) }
