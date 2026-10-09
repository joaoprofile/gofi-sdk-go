// Package grpcapi implements people.v1.PeopleService over the people store.
// Handlers return the store's errs.AppError as is: grpcx turns it into the
// matching gRPC status (NotFound, InvalidArgument, ...) and the client gets
// the same code back with grpcx.FromError.
package grpcapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/gen/people/v1"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/people"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
)

// DevToken is the bearer token both commands use. A real service validates a
// JWT or an mTLS identity instead.
const DevToken = "secret-token"

// chunkSize is the size of each image chunk sent by DownloadPhoto.
const chunkSize = 32 << 10 // 32 KiB

// AuthConfig requires DevToken on every call. Health checks are always
// public; reflection is listed so grpcurl can discover the API.
func AuthConfig() *grpcx.AuthConfig {
	return &grpcx.AuthConfig{
		Validate: func(ctx context.Context, token string) (context.Context, error) {
			if subtle.ConstantTimeCompare([]byte(token), []byte(DevToken)) != 1 {
				return nil, errors.New("invalid token")
			}
			return ctx, nil
		},
		PublicMethods: []string{
			"/grpc.reflection.v1.ServerReflection/",
			"/grpc.reflection.v1alpha.ServerReflection/",
		},
	}
}

// Register registers the service; pass it to grpcserver.Component.Register.
func Register(store *people.Store) func(grpc.ServiceRegistrar) {
	return func(r grpc.ServiceRegistrar) {
		peoplev1.RegisterPeopleServiceServer(r, &service{store: store})
	}
}

type service struct {
	peoplev1.UnimplementedPeopleServiceServer
	store *people.Store
}

func (s *service) CreatePerson(_ context.Context, req *peoplev1.CreatePersonRequest) (*peoplev1.Person, error) {
	in := people.NewPerson{
		Name:   req.GetName(),
		Email:  req.GetEmail(),
		Phones: req.GetPhones(),
		Address: people.Address{
			Street:  req.GetAddress().GetStreet(),
			City:    req.GetAddress().GetCity(),
			Country: req.GetAddress().GetCountry(),
		},
	}
	if req.GetBirthDate() != nil {
		in.BirthDate = req.GetBirthDate().AsTime()
	}
	p, err := s.store.Create(in)
	if err != nil {
		return nil, err
	}
	return toProto(p), nil
}

func (s *service) GetPerson(_ context.Context, req *peoplev1.GetPersonRequest) (*peoplev1.Person, error) {
	p, err := s.store.Get(req.GetId())
	if err != nil {
		return nil, err
	}
	return toProto(p), nil
}

// ListPeople sends one message per person; the client reads until io.EOF.
func (s *service) ListPeople(req *peoplev1.ListPeopleRequest, stream grpc.ServerStreamingServer[peoplev1.Person]) error {
	for _, p := range s.store.List(req.GetNameContains()) {
		if err := stream.Send(toProto(p)); err != nil {
			return err // the client went away
		}
	}
	return nil
}

// UploadPhoto reads the person ID, then the image chunks until the client
// closes its side, and answers once with the stored photo's metadata.
func (s *service) UploadPhoto(stream grpc.ClientStreamingServer[peoplev1.UploadPhotoRequest, peoplev1.PhotoInfo]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	id := first.GetPersonId()
	if id == "" {
		return people.ErrInvalidPhoto.New("the first message must carry person_id")
	}
	if _, err := s.store.Get(id); err != nil {
		return err // fail before receiving the whole image
	}

	var data []byte
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		chunk := msg.GetChunk()
		if len(data)+len(chunk) > people.MaxPhotoSize {
			return people.ErrInvalidPhoto.New(fmt.Sprintf("larger than %d bytes", people.MaxPhotoSize))
		}
		data = append(data, chunk...)
	}

	info, err := s.store.SetPhoto(id, data)
	if err != nil {
		return err
	}
	return stream.SendAndClose(photoInfoToProto(info))
}

// DownloadPhoto sends the metadata first, then the image in chunks.
func (s *service) DownloadPhoto(req *peoplev1.DownloadPhotoRequest, stream grpc.ServerStreamingServer[peoplev1.DownloadPhotoResponse]) error {
	info, data, err := s.store.Photo(req.GetPersonId())
	if err != nil {
		return err
	}
	if err := stream.Send(&peoplev1.DownloadPhotoResponse{
		Data: &peoplev1.DownloadPhotoResponse_Info{Info: photoInfoToProto(info)},
	}); err != nil {
		return err
	}
	for start := 0; start < len(data); start += chunkSize {
		end := min(start+chunkSize, len(data))
		if err := stream.Send(&peoplev1.DownloadPhotoResponse{
			Data: &peoplev1.DownloadPhotoResponse_Chunk{Chunk: data[start:end]},
		}); err != nil {
			return err
		}
	}
	return nil
}

// Chat greets the caller as soon as the stream opens, then answers every
// message until the client closes its side.
func (s *service) Chat(stream grpc.BidiStreamingServer[peoplev1.ChatMessage, peoplev1.ChatMessage]) error {
	if err := stream.Send(serverMessage("connected, say something")); err != nil {
		return err
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		p, err := s.store.Get(msg.GetPersonId())
		if err != nil {
			return err // ends the stream with NotFound
		}
		reply := fmt.Sprintf("hi %s, you said %q", p.Name, msg.GetText())
		if err := stream.Send(serverMessage(reply)); err != nil {
			return err
		}
	}
}

func serverMessage(text string) *peoplev1.ChatMessage {
	return &peoplev1.ChatMessage{PersonId: "server", Text: text, SentAt: timestamppb.Now()}
}

func toProto(p people.Person) *peoplev1.Person {
	out := &peoplev1.Person{
		Id:     p.ID,
		Name:   p.Name,
		Email:  p.Email,
		Phones: p.Phones,
		Address: &peoplev1.Address{
			Street:  p.Address.Street,
			City:    p.Address.City,
			Country: p.Address.Country,
		},
		CreatedAt: timestamppb.New(p.CreatedAt),
	}
	if !p.BirthDate.IsZero() {
		out.BirthDate = timestamppb.New(p.BirthDate)
	}
	if p.Photo != nil {
		out.Photo = photoInfoToProto(*p.Photo)
	}
	return out
}

func photoInfoToProto(info people.PhotoInfo) *peoplev1.PhotoInfo {
	return &peoplev1.PhotoInfo{ContentType: info.ContentType, Size: info.Size, Sha256: info.SHA256}
}
