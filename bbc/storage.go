package bbc

import (
	"fmt"
	"github.com/SharzyL/bbc/bbc/pb"
	"os"
	"path"
	"path/filepath"

	"google.golang.org/protobuf/proto"
)

type storageMgr struct {
	Folder string

	blockDir string
}

func newStorageMgr(folder string) (*storageMgr, error) {
	mgr := &storageMgr{Folder: folder, blockDir: path.Join(folder, "blocks")}
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		return nil, fmt.Errorf("storage dir %s do not exists", folder)
	}
	if _, err := os.Stat(mgr.blockDir); os.IsNotExist(err) {
		if err := os.Mkdir(path.Join(folder, "blocks"), 0755); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

func (s *storageMgr) LoadBlock(height int64) (*pb.FullBlock, error) {
	return s.readBlock(s.getBlockPath(height))
}

func (s *storageMgr) DumpBlock(b *pb.FullBlock) error {
	return s.writeBlock(s.getBlockPath(b.Header.Height), b)
}

func (s *storageMgr) getBlockPath(height int64) string {
	return path.Join(s.blockDir, fmt.Sprintf("%08d.block", height))
}

func (s *storageMgr) readBlock(fileName string) (*pb.FullBlock, error) {
	binData, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	b := &pb.FullBlock{}
	err = proto.Unmarshal(binData, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *storageMgr) writeBlock(fileName string, b *pb.FullBlock) error {
	fp, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	binData, err := proto.Marshal(b)
	if err != nil {
		return err
	}
	_, err = fp.Write(binData)
	if err != nil {
		return err
	}

	return nil
}

func (s *storageMgr) removeBlock(height int64) error {
	return os.Remove(s.getBlockPath(height))
}

func (s *storageMgr) cleanBlocks() error {
	files, err := filepath.Glob(filepath.Join(s.blockDir, "*"))
	if err != nil {
		return err
	}
	for _, file := range files {
		err = os.Remove(file)
		if err != nil {
			return err
		}
	}
	return nil
}
