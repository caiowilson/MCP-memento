package indexing

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	vectorMagic        = "MMVEC001"
	maxVectorDimension = 16_384
	maxVectorsPerFile  = 100_000
)

type chunkVector struct {
	StartLine int
	EndLine   int
	Values    []float32
}

func (i *Indexer) vectorFilePath(id string) string {
	return filepath.Join(i.dir, "files", id+".vec")
}

func (i *Indexer) writeVectorsFile(id, fingerprint string, vectors []chunkVector) error {
	if len(vectors) == 0 {
		return errors.New("cannot persist an empty vector set")
	}
	if len(fingerprint) == 0 || len(fingerprint) > math.MaxUint16 {
		return errors.New("invalid embedding fingerprint")
	}
	dimension := len(vectors[0].Values)
	if dimension == 0 || dimension > maxVectorDimension {
		return fmt.Errorf("invalid vector dimension %d", dimension)
	}
	for index, vector := range vectors {
		if len(vector.Values) != dimension {
			return fmt.Errorf("vector %d has dimension %d, expected %d", index, len(vector.Values), dimension)
		}
	}

	path := i.vectorFilePath(id)
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	writer := bufio.NewWriterSize(file, 64*1024)
	fail := func(writeErr error) error {
		_ = file.Close()
		_ = os.Remove(tmp)
		return writeErr
	}
	if _, err := writer.WriteString(vectorMagic); err != nil {
		return fail(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, uint16(len(fingerprint))); err != nil {
		return fail(err)
	}
	if _, err := writer.WriteString(fingerprint); err != nil {
		return fail(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(dimension)); err != nil {
		return fail(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(vectors))); err != nil {
		return fail(err)
	}
	for _, vector := range vectors {
		if err := binary.Write(writer, binary.LittleEndian, int32(vector.StartLine)); err != nil {
			return fail(err)
		}
		if err := binary.Write(writer, binary.LittleEndian, int32(vector.EndLine)); err != nil {
			return fail(err)
		}
		for _, value := range vector.Values {
			if err := binary.Write(writer, binary.LittleEndian, math.Float32bits(value)); err != nil {
				return fail(err)
			}
		}
	}
	if err := writer.Flush(); err != nil {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (i *Indexer) readVectorsFile(id string) ([]chunkVector, error) {
	file, err := os.Open(i.vectorFilePath(id))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	magic := make([]byte, len(vectorMagic))
	if _, err := io.ReadFull(reader, magic); err != nil {
		return nil, err
	}
	if string(magic) != vectorMagic {
		return nil, errors.New("unsupported vector file format")
	}
	var fingerprintLength uint16
	if err := binary.Read(reader, binary.LittleEndian, &fingerprintLength); err != nil {
		return nil, err
	}
	fingerprint := make([]byte, int(fingerprintLength))
	if _, err := io.ReadFull(reader, fingerprint); err != nil {
		return nil, err
	}
	if string(fingerprint) != i.embeddingFingerprint() {
		return nil, errors.New("vector file embedding fingerprint mismatch")
	}
	var dimension, count uint32
	if err := binary.Read(reader, binary.LittleEndian, &dimension); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	if dimension == 0 || dimension > maxVectorDimension || count > maxVectorsPerFile {
		return nil, fmt.Errorf("invalid vector file dimensions: dimension=%d count=%d", dimension, count)
	}
	vectors := make([]chunkVector, int(count))
	for index := range vectors {
		var startLine, endLine int32
		if err := binary.Read(reader, binary.LittleEndian, &startLine); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &endLine); err != nil {
			return nil, err
		}
		if startLine < 1 || endLine < startLine {
			return nil, errors.New("invalid vector line range")
		}
		values := make([]float32, int(dimension))
		for valueIndex := range values {
			var bits uint32
			if err := binary.Read(reader, binary.LittleEndian, &bits); err != nil {
				return nil, err
			}
			value := math.Float32frombits(bits)
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, errors.New("vector file contains a non-finite value")
			}
			values[valueIndex] = value
		}
		vectors[index] = chunkVector{StartLine: int(startLine), EndLine: int(endLine), Values: values}
	}
	return vectors, nil
}

func (i *Indexer) removeVectorFile(id string) {
	_ = os.Remove(i.vectorFilePath(id))
}

func (i *Indexer) resetVectorFiles() error {
	paths, err := filepath.Glob(filepath.Join(i.dir, "files", "*.vec"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for rel, entry := range i.manifest.Files {
		entry.Vectors = 0
		i.manifest.Files[rel] = entry
	}
	i.manifest.EmbeddingFingerprint = i.embeddingFingerprint()
	return nil
}
