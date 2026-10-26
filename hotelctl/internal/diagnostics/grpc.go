package diagnostics

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflection "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// CheckEcho probes a concrete transport route without mutating business data.
func CheckEcho(ctx context.Context, address, method, serviceName string) error {
	return echoRPC(ctx, address, method, serviceName)
}

// Resolve the schema from the server instead of importing service internals or
// relying on coincidentally compatible protobuf wire layouts.
func echoRPC(ctx context.Context, address, method, serviceName string) error {
	parts := strings.Split(strings.TrimPrefix(method, "/"), "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid Echo method %q", method)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := reflection.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	if err = stream.Send(&reflection.ServerReflectionRequest{
		MessageRequest: &reflection.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: parts[0],
		},
	}); err != nil {
		return err
	}
	response, err := stream.Recv()
	if err != nil {
		return err
	}
	if failure := response.GetErrorResponse(); failure != nil {
		return fmt.Errorf("reflection: %s", failure.ErrorMessage)
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, raw := range response.GetFileDescriptorResponse().GetFileDescriptorProto() {
		file := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(raw, file); err != nil {
			return err
		}
		set.File = append(set.File, file)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return err
	}
	descriptor, err := files.FindDescriptorByName(protoreflect.FullName(parts[0]))
	if err != nil {
		return err
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		return fmt.Errorf("%s is not a gRPC service", parts[0])
	}
	rpc := service.Methods().ByName(protoreflect.Name(parts[1]))
	if rpc == nil {
		return fmt.Errorf("Echo method %s is missing", method)
	}
	input, output := rpc.Input().Fields().ByName("message"), rpc.Output().Fields().ByName("message")
	if input == nil ||
		output == nil ||
		input.Kind() != protoreflect.StringKind ||
		output.Kind() != protoreflect.StringKind ||
		input.IsList() ||
		output.IsList() {
		return fmt.Errorf("%s must have a string message in request and response", method)
	}
	const message = "Привет, HotelFlow!"
	request := dynamicpb.NewMessage(rpc.Input())
	request.Set(input, protoreflect.ValueOfString(message))
	result := dynamicpb.NewMessage(rpc.Output())
	if err := conn.Invoke(ctx, "/"+strings.TrimPrefix(method, "/"), request, result); err != nil {
		return err
	}
	if result.Get(output).String() != message {
		return fmt.Errorf("%s returned a different message", method)
	}
	if identity := rpc.Output().Fields().ByName("service"); identity != nil {
		if identity.Kind() != protoreflect.StringKind ||
			identity.IsList() ||
			result.Get(identity).String() != serviceName {
			return fmt.Errorf("%s response came from an unexpected service", method)
		}
	}
	return nil
}
