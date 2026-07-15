// Package crypto provides AES-256-GCM encryption and framed message transport.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
)

// AuthToken and AuthResponse are the handshake bytes exchanged at connect time.
var (
	AuthToken    = []byte("HATURAYA_AUTH_v1")
	AuthResponse = []byte("HATURAYA_OK_v1")
)

// Cipher wraps an AES-256-GCM key.
type Cipher struct {
	key []byte
}

// New generates a fresh 32-byte random key.
func New() (*Cipher, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("key gen: %w", err)
	}
	return &Cipher{key: key}, nil
}

// NewWithKey builds a Cipher from an existing 32-byte key.
func NewWithKey(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	k := make([]byte, 32)
	copy(k, key)
	return &Cipher{key: k}, nil
}

// Key returns a copy of the raw key bytes.
func (c *Cipher) Key() []byte {
	k := make([]byte, len(c.key))
	copy(k, c.key)
	return k
}

// KeyHex returns the key as a lowercase hex string.
func (c *Cipher) KeyHex() string {
	return hex.EncodeToString(c.key)
}

// Encrypt encrypts data with AES-256-GCM.
// Output: 12-byte random nonce ++ ciphertext+tag.
func (c *Cipher) Encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, data, nil)
	return append(nonce, ct...), nil
}

// Decrypt decrypts AES-256-GCM ciphertext where the first 12 bytes are the nonce.
func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, fmt.Errorf("ciphertext too short (%d bytes)", len(data))
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}

// SendMsg encrypts data, then writes a 4-byte big-endian length header followed
// by the ciphertext over conn.
func SendMsg(conn net.Conn, ciph *Cipher, data []byte) error {
	enc, err := ciph.Encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(enc)))
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	_, err = conn.Write(enc)
	return err
}

// RecvMsg reads a 4-byte big-endian length from conn, reads that many bytes,
// then decrypts and returns the plaintext.
func RecvMsg(conn net.Conn, ciph *Cipher) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, fmt.Errorf("recv hdr: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr)
	if n > 10*1024*1024 {
		return nil, fmt.Errorf("message too large: %d bytes", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("recv body: %w", err)
	}
	return ciph.Decrypt(buf)
}

// SendRaw writes data directly to conn without any framing or encryption.
func SendRaw(conn net.Conn, data []byte) error {
	_, err := conn.Write(data)
	return err
}
