package hedge

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
)

// attemptResult хранит ответ и metadata одной попытки.
type attemptResult struct {
	reply    proto.Message
	headers  metadata.MD
	trailers metadata.MD
	peer     peer.Peer
}

// callOptions отделяет выходные указатели вызывающего кода от options попыток.
type callOptions struct {
	input          []grpc.CallOption
	headerTargets  []*metadata.MD
	trailerTargets []*metadata.MD
	peerTargets    []*peer.Peer
	onFinish       []func(error)
}

func newCallOptions(options []grpc.CallOption) callOptions {
	result := callOptions{
		input: make([]grpc.CallOption, 0, len(options)),
	}
	for _, option := range options {
		switch o := option.(type) {
		case grpc.HeaderCallOption:
			result.headerTargets = append(result.headerTargets, o.HeaderAddr)
		case *grpc.HeaderCallOption:
			result.headerTargets = append(result.headerTargets, o.HeaderAddr)
		case grpc.TrailerCallOption:
			result.trailerTargets = append(result.trailerTargets, o.TrailerAddr)
		case *grpc.TrailerCallOption:
			result.trailerTargets = append(result.trailerTargets, o.TrailerAddr)
		case grpc.PeerCallOption:
			result.peerTargets = append(result.peerTargets, o.PeerAddr)
		case *grpc.PeerCallOption:
			result.peerTargets = append(result.peerTargets, o.PeerAddr)
		case grpc.OnFinishCallOption:
			result.onFinish = append(result.onFinish, o.OnFinish)
		case *grpc.OnFinishCallOption:
			result.onFinish = append(result.onFinish, o.OnFinish)
		default:
			result.input = append(result.input, option)
		}
	}
	return result
}

// forAttempt создаёт отдельный срез options с metadata конкретной попытки.
func (o callOptions) forAttempt(response *attemptResult) []grpc.CallOption {
	options := make([]grpc.CallOption, len(o.input), len(o.input)+3)
	copy(options, o.input)
	return append(options, grpc.Header(&response.headers), grpc.Trailer(&response.trailers), grpc.Peer(&response.peer))
}

// copyOutput вызывается только для выбранной попытки, в том числе при ошибке.
func (o callOptions) copyOutput(response *attemptResult) {
	for _, target := range o.headerTargets {
		*target = response.headers.Copy()
	}
	for _, target := range o.trailerTargets {
		*target = response.trailers.Copy()
	}
	for _, target := range o.peerTargets {
		*target = response.peer
	}
}

// finish уведомляет о результате логического RPC, а не отдельных попыток.
func (o callOptions) finish(err error) {
	for _, callback := range o.onFinish {
		if callback != nil {
			callback(err)
		}
	}
}
