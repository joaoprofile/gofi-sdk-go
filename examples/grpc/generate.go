// Package grpcexample holds the generate directive; the commands live in
// grpc-server, http-grpc-server and client.
//
// Regenerate gen/ after editing the proto (needs protoc, protoc-gen-go and
// protoc-gen-go-grpc on PATH):
//
//	go generate .
package grpcexample

//go:generate protoc --proto_path=proto --go_out=gen --go_opt=paths=source_relative --go-grpc_out=gen --go-grpc_opt=paths=source_relative people/v1/people.proto
