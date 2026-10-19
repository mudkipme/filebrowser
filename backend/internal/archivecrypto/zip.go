// Package archivecrypto opens encrypted ZIP entries while keeping archive/zip's
// parser and the application's filename decoding. Encryption formats follow:
// https://www.winzip.com/en/support/aes-encryption/
// https://pkware.cachefly.net/webdocs/casestudies/APPNOTE.TXT (section 6.1).
package archivecrypto

import (
	"archive/zip"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1" // Required by the WinZip AES format.
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"math"
)

var (
	ErrPassword       = errors.New("ZIP password is missing or incorrect")
	ErrAuthentication = errors.New("encrypted ZIP authentication failed")
)

// Open supports traditional ZipCrypto and WinZip AES (AE-1 and AE-2), with
// stored or DEFLATE-compressed contents. Other entries use archive/zip directly.
// The caller must keep the containing zip.Reader open until this reader closes.
func Open(ctx context.Context, f *zip.File, password string) (io.ReadCloser, error) {
	if f.Flags&1 == 0 {
		return f.Open()
	}
	if password == "" {
		return nil, ErrPassword
	}
	if f.Flags&(1<<6) != 0 { // PKWARE strong encryption is a different format.
		return nil, zip.ErrAlgorithm
	}
	if f.CompressedSize64 > math.MaxInt64 || f.UncompressedSize64 >= math.MaxInt64 {
		return nil, zip.ErrFormat
	}
	raw, err := f.OpenRaw()
	if err != nil {
		return nil, err
	}
	var src io.Reader = contextReader{ctx, raw}
	method, checkCRC := f.Method, true
	var verify func() error
	if method == 99 {
		var authenticated *aesReader
		authenticated, method, checkCRC, err = openAES(ctx, f, src, password)
		if err == nil {
			src, verify = authenticated, authenticated.verify
		}
	} else {
		src, err = openZipCrypto(f, src, password)
	}
	if err != nil {
		return nil, err
	}
	var rc io.ReadCloser
	switch method {
	case zip.Store:
		rc = io.NopCloser(src)
	case zip.Deflate:
		rc = flate.NewReader(src)
	default:
		return nil, zip.ErrAlgorithm
	}
	return &checkedReader{ReadCloser: rc, sum: crc32.NewIEEE(), size: f.UncompressedSize64, crc: f.CRC32, checkCRC: checkCRC, verify: verify}, nil
}

func openAES(ctx context.Context, f *zip.File, raw io.Reader, password string) (*aesReader, uint16, bool, error) {
	var version, method uint16
	var strength byte
	for extra := f.Extra; len(extra) >= 4; {
		tag, size := binary.LittleEndian.Uint16(extra), int(binary.LittleEndian.Uint16(extra[2:]))
		extra = extra[4:]
		if size > len(extra) {
			return nil, 0, false, zip.ErrFormat
		}
		if tag == 0x9901 {
			if size != 7 || string(extra[2:4]) != "AE" || version != 0 {
				return nil, 0, false, zip.ErrFormat
			}
			version = binary.LittleEndian.Uint16(extra)
			strength = extra[4]
			method = binary.LittleEndian.Uint16(extra[5:])
		}
		extra = extra[size:]
	}
	if (version != 1 && version != 2) || strength < 1 || strength > 3 {
		return nil, 0, false, zip.ErrFormat
	}
	keyLen := 8 * (int(strength) + 1)
	headerLen := keyLen/2 + 2 // Salt followed by two password verification bytes.
	if f.CompressedSize64 < uint64(headerLen+10) {
		return nil, 0, false, zip.ErrFormat
	}
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(raw, header); err != nil {
		return nil, 0, false, err
	}
	keys, err := pbkdf2.Key(sha1.New, password, header[:headerLen-2], 1000, 2*keyLen+2)
	if err != nil {
		return nil, 0, false, err
	}
	if subtle.ConstantTimeCompare(header[headerLen-2:], keys[2*keyLen:]) != 1 {
		return nil, 0, false, ErrPassword
	}
	dataLen := int64(f.CompressedSize64) - int64(headerLen) - 10
	mac := hmac.New(sha1.New, keys[keyLen:2*keyLen])
	// Authenticate the ciphertext in bounded memory before releasing plaintext.
	// Reopen the same entry afterwards for streaming decryption.
	if _, err = io.CopyN(mac, raw, dataLen); err != nil {
		return nil, 0, false, err
	}
	var tag [10]byte
	if _, err = io.ReadFull(raw, tag[:]); err != nil {
		return nil, 0, false, err
	}
	if !hmac.Equal(mac.Sum(nil)[:10], tag[:]) {
		return nil, 0, false, ErrAuthentication
	}
	raw, err = f.OpenRaw()
	if err != nil {
		return nil, 0, false, err
	}
	raw = contextReader{ctx, raw}
	if _, err = io.CopyN(io.Discard, raw, int64(headerLen)); err != nil {
		return nil, 0, false, err
	}
	block, err := aes.NewCipher(keys[:keyLen])
	if err != nil {
		return nil, 0, false, err
	}
	stream := &winZipCTR{block: block, used: aes.BlockSize}
	stream.counter[0] = 1
	mac = hmac.New(sha1.New, keys[keyLen:2*keyLen])
	ciphertext := io.TeeReader(io.LimitReader(raw, dataLen), mac)
	return &aesReader{
		Reader:     &cipher.StreamReader{S: stream, R: ciphertext},
		ciphertext: ciphertext,
		mac:        mac,
		tag:        tag,
	}, method, version == 1, nil
}

type aesReader struct {
	io.Reader
	ciphertext io.Reader
	mac        hash.Hash
	tag        [10]byte
}

func (r *aesReader) verify() error {
	// DEFLATE can stop without reading to ciphertext EOF. Drain what remains
	// and authenticate again so concurrent source changes cannot go unnoticed.
	if _, err := io.Copy(io.Discard, r.ciphertext); err != nil {
		return err
	}
	if !hmac.Equal(r.mac.Sum(nil)[:10], r.tag[:]) {
		return ErrAuthentication
	}
	return nil
}

// WinZip uses a little-endian counter starting at 1; cipher.NewCTR uses a
// big-endian counter, so it cannot directly decode this format.
type winZipCTR struct {
	block   cipher.Block
	counter [aes.BlockSize]byte
	stream  [aes.BlockSize]byte
	used    int
}

func (s *winZipCTR) XORKeyStream(dst, src []byte) {
	for i, value := range src {
		if s.used == aes.BlockSize {
			s.block.Encrypt(s.stream[:], s.counter[:])
			for j := range s.counter {
				s.counter[j]++
				if s.counter[j] != 0 {
					break
				}
			}
			s.used = 0
		}
		dst[i] = value ^ s.stream[s.used]
		s.used++
	}
}

type zipCryptoReader struct {
	r    io.Reader
	keys [3]uint32
}

func (r *zipCryptoReader) update(value byte) {
	r.keys[0] = crc32.IEEETable[byte(r.keys[0])^value] ^ (r.keys[0] >> 8)
	r.keys[1] = (r.keys[1]+uint32(byte(r.keys[0])))*134775813 + 1
	r.keys[2] = crc32.IEEETable[byte(r.keys[2])^byte(r.keys[1]>>24)] ^ (r.keys[2] >> 8)
}

func (r *zipCryptoReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	for i := range p[:n] {
		temp := r.keys[2] | 2
		p[i] ^= byte((temp * (temp ^ 1)) >> 8)
		r.update(p[i])
	}
	return n, err
}

func openZipCrypto(f *zip.File, src io.Reader, password string) (io.Reader, error) {
	if f.CompressedSize64 < 12 {
		return nil, zip.ErrFormat
	}
	r := &zipCryptoReader{r: src, keys: [3]uint32{0x12345678, 0x23456789, 0x34567890}}
	for _, value := range []byte(password) {
		r.update(value)
	}
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	check := byte(f.CRC32 >> 24)
	if f.Flags&8 != 0 {
		check = byte(f.ModifiedTime >> 8)
	}
	if header[11] != check {
		return nil, ErrPassword
	}
	return r, nil
}

type checkedReader struct {
	io.ReadCloser
	sum      hash.Hash32
	read     uint64
	size     uint64
	crc      uint32
	checkCRC bool
	verify   func() error
}

func (r *checkedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += uint64(n)
	_, _ = r.sum.Write(p[:n])
	if r.read > r.size {
		return n, zip.ErrFormat
	}
	if err == io.EOF {
		if r.read != r.size {
			return n, io.ErrUnexpectedEOF
		}
		if r.checkCRC && r.sum.Sum32() != r.crc {
			return n, zip.ErrChecksum
		}
		if r.verify != nil {
			if verifyErr := r.verify(); verifyErr != nil {
				return n, verifyErr
			}
		}
	}
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
