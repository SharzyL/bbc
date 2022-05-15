// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.20.1
// source: bbc.proto

package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// MinerClient is the client API for Miner service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type MinerClient interface {
	PeekChain(ctx context.Context, in *PeekChainReq, opts ...grpc.CallOption) (*PeekChainAns, error)
	AdvertiseBlock(ctx context.Context, in *AdvertiseBlockReq, opts ...grpc.CallOption) (*AdvertiseBlockAns, error)
	GetFullBlock(ctx context.Context, in *HashVal, opts ...grpc.CallOption) (*FullBlock, error)
	FindTx(ctx context.Context, in *HashVal, opts ...grpc.CallOption) (*TxInfo, error)
	PeekChainByHeight(ctx context.Context, in *PeekChainByHeightReq, opts ...grpc.CallOption) (*PeekChainByHeightAns, error)
	UploadTx(ctx context.Context, in *Tx, opts ...grpc.CallOption) (*UploadTxAns, error)
}

type minerClient struct {
	cc grpc.ClientConnInterface
}

func NewMinerClient(cc grpc.ClientConnInterface) MinerClient {
	return &minerClient{cc}
}

func (c *minerClient) PeekChain(ctx context.Context, in *PeekChainReq, opts ...grpc.CallOption) (*PeekChainAns, error) {
	out := new(PeekChainAns)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/PeekChain", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *minerClient) AdvertiseBlock(ctx context.Context, in *AdvertiseBlockReq, opts ...grpc.CallOption) (*AdvertiseBlockAns, error) {
	out := new(AdvertiseBlockAns)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/AdvertiseBlock", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *minerClient) GetFullBlock(ctx context.Context, in *HashVal, opts ...grpc.CallOption) (*FullBlock, error) {
	out := new(FullBlock)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/GetFullBlock", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *minerClient) FindTx(ctx context.Context, in *HashVal, opts ...grpc.CallOption) (*TxInfo, error) {
	out := new(TxInfo)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/FindTx", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *minerClient) PeekChainByHeight(ctx context.Context, in *PeekChainByHeightReq, opts ...grpc.CallOption) (*PeekChainByHeightAns, error) {
	out := new(PeekChainByHeightAns)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/PeekChainByHeight", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *minerClient) UploadTx(ctx context.Context, in *Tx, opts ...grpc.CallOption) (*UploadTxAns, error) {
	out := new(UploadTxAns)
	err := c.cc.Invoke(ctx, "/bbc_proto.Miner/UploadTx", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MinerServer is the server API for Miner service.
// All implementations must embed UnimplementedMinerServer
// for forward compatibility
type MinerServer interface {
	PeekChain(context.Context, *PeekChainReq) (*PeekChainAns, error)
	AdvertiseBlock(context.Context, *AdvertiseBlockReq) (*AdvertiseBlockAns, error)
	GetFullBlock(context.Context, *HashVal) (*FullBlock, error)
	FindTx(context.Context, *HashVal) (*TxInfo, error)
	PeekChainByHeight(context.Context, *PeekChainByHeightReq) (*PeekChainByHeightAns, error)
	UploadTx(context.Context, *Tx) (*UploadTxAns, error)
	mustEmbedUnimplementedMinerServer()
}

// UnimplementedMinerServer must be embedded to have forward compatible implementations.
type UnimplementedMinerServer struct {
}

func (UnimplementedMinerServer) PeekChain(context.Context, *PeekChainReq) (*PeekChainAns, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PeekChain not implemented")
}
func (UnimplementedMinerServer) AdvertiseBlock(context.Context, *AdvertiseBlockReq) (*AdvertiseBlockAns, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AdvertiseBlock not implemented")
}
func (UnimplementedMinerServer) GetFullBlock(context.Context, *HashVal) (*FullBlock, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetFullBlock not implemented")
}
func (UnimplementedMinerServer) FindTx(context.Context, *HashVal) (*TxInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method FindTx not implemented")
}
func (UnimplementedMinerServer) PeekChainByHeight(context.Context, *PeekChainByHeightReq) (*PeekChainByHeightAns, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PeekChainByHeight not implemented")
}
func (UnimplementedMinerServer) UploadTx(context.Context, *Tx) (*UploadTxAns, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UploadTx not implemented")
}
func (UnimplementedMinerServer) mustEmbedUnimplementedMinerServer() {}

// UnsafeMinerServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to MinerServer will
// result in compilation errors.
type UnsafeMinerServer interface {
	mustEmbedUnimplementedMinerServer()
}

func RegisterMinerServer(s grpc.ServiceRegistrar, srv MinerServer) {
	s.RegisterService(&Miner_ServiceDesc, srv)
}

func _Miner_PeekChain_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PeekChainReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).PeekChain(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/PeekChain",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).PeekChain(ctx, req.(*PeekChainReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Miner_AdvertiseBlock_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AdvertiseBlockReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).AdvertiseBlock(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/AdvertiseBlock",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).AdvertiseBlock(ctx, req.(*AdvertiseBlockReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Miner_GetFullBlock_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HashVal)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).GetFullBlock(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/GetFullBlock",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).GetFullBlock(ctx, req.(*HashVal))
	}
	return interceptor(ctx, in, info, handler)
}

func _Miner_FindTx_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HashVal)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).FindTx(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/FindTx",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).FindTx(ctx, req.(*HashVal))
	}
	return interceptor(ctx, in, info, handler)
}

func _Miner_PeekChainByHeight_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PeekChainByHeightReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).PeekChainByHeight(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/PeekChainByHeight",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).PeekChainByHeight(ctx, req.(*PeekChainByHeightReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Miner_UploadTx_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Tx)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MinerServer).UploadTx(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bbc_proto.Miner/UploadTx",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MinerServer).UploadTx(ctx, req.(*Tx))
	}
	return interceptor(ctx, in, info, handler)
}

// Miner_ServiceDesc is the grpc.ServiceDesc for Miner service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Miner_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bbc_proto.Miner",
	HandlerType: (*MinerServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "PeekChain",
			Handler:    _Miner_PeekChain_Handler,
		},
		{
			MethodName: "AdvertiseBlock",
			Handler:    _Miner_AdvertiseBlock_Handler,
		},
		{
			MethodName: "GetFullBlock",
			Handler:    _Miner_GetFullBlock_Handler,
		},
		{
			MethodName: "FindTx",
			Handler:    _Miner_FindTx_Handler,
		},
		{
			MethodName: "PeekChainByHeight",
			Handler:    _Miner_PeekChainByHeight_Handler,
		},
		{
			MethodName: "UploadTx",
			Handler:    _Miner_UploadTx_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "bbc.proto",
}
