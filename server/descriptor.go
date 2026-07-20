package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/openmock-dev/openmock-go/engine"
)

// grpcSchema is the compiled protobuf schema for one gRPC server, loaded from
// its descriptorSet (spec §3.4). It maps each declared "package.Service/Method"
// to its method descriptor, which carries the input/output message types the
// transport uses to decode and encode wire bytes.
type grpcSchema struct {
	methods map[string]protoreflect.MethodDescriptor
}

// method returns the descriptor for a "package.Service/Method" key, if the
// schema declares it.
func (s *grpcSchema) method(service, rpc string) (protoreflect.MethodDescriptor, bool) {
	md, ok := s.methods[service+"/"+rpc]
	return md, ok
}

// loadDescriptor resolves a gRPC server's descriptorSet relative to baseDir
// and builds its schema. The path is confined to baseDir per spec §14: an
// absolute path or one escaping via ".." is rejected. A missing or unreadable
// descriptor is not fatal to the caller — it is reported as an error so the
// server can be left unbound with a diagnostic, exactly as if the field were
// absent.
func loadDescriptor(baseDir string, srv *engine.Server) (*grpcSchema, error) {
	path, err := resolveDescriptorPath(baseDir, srv.DescriptorSet)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &fds); err != nil {
		return nil, fmt.Errorf("parsing descriptor set %s: %w", srv.DescriptorSet, err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("building descriptors from %s: %w", srv.DescriptorSet, err)
	}

	schema := &grpcSchema{methods: map[string]protoreflect.MethodDescriptor{}}
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			name := string(svc.FullName())
			rpcs := svc.Methods()
			for j := 0; j < rpcs.Len(); j++ {
				m := rpcs.Get(j)
				schema.methods[name+"/"+string(m.Name())] = m
			}
		}
		return true
	})
	return schema, nil
}

// resolveDescriptorPath resolves a gRPC descriptorSet path (spec §14).
func resolveDescriptorPath(baseDir, ref string) (string, error) {
	return confinedPath(baseDir, ref, "descriptorSet")
}

// confinedPath joins ref onto baseDir and rejects a path that escapes it
// (spec §14): absolute paths and ".." traversal are refused, and a remote URL
// is not dereferenced. field names the document field for diagnostics
// (descriptorSet, schema).
func confinedPath(baseDir, ref, field string) (string, error) {
	if strings.Contains(ref, "://") {
		return "", fmt.Errorf("%s %q: remote URLs are not loaded", field, ref)
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("%s %q: absolute paths are not allowed", field, ref)
	}
	if baseDir == "" {
		baseDir = "."
	}
	cleanBase := filepath.Clean(baseDir)
	joined := filepath.Clean(filepath.Join(cleanBase, ref))
	// joined must be cleanBase itself or sit beneath it.
	rel, err := filepath.Rel(cleanBase, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s %q escapes the document directory", field, ref)
	}
	return joined, nil
}
