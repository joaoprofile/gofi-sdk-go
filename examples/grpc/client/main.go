// Command client calls people.v1.PeopleService through every call shape:
// unary, server streaming, client streaming (photo upload in chunks), server
// streaming (photo download in chunks) and bidirectional streaming (chat). It
// works against grpc-server and http-grpc-server alike.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/gen/people/v1"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/grpcapi"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
)

// uploadChunkSize is small on purpose so even a small image takes several
// messages.
const uploadChunkSize = 16 << 10 // 16 KiB

func main() {
	addr := flag.String("addr", "localhost:9090", "gRPC server address")
	imagePath := flag.String("image", "", "photo to upload (PNG, JPEG, GIF or WebP); empty generates a PNG")
	out := flag.String("out", "downloaded-photo.png", "where to save the downloaded photo")
	flag.Parse()

	// The connection is lazy and reused by every call; close it on exit.
	conn, err := grpcx.NewClient("dns:///"+*addr, grpcx.ClientConfig{
		Insecure: true, // the servers serve h2c in dev; use TLS: &grpcx.ClientTLSConfig{...} otherwise
		Token:    func(context.Context) (string, error) { return grpcapi.DevToken, nil },
		Timeout:  5 * time.Second, // unary calls without a deadline
	})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := peoplev1.NewPeopleServiceClient(conn)

	// Streams have no default deadline: bound the whole run.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := run(ctx, client, *imagePath, *out); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, client peoplev1.PeopleServiceClient, imagePath, out string) error {
	step("1. unary: CreatePerson")
	ada, err := client.CreatePerson(ctx, &peoplev1.CreatePersonRequest{
		Name:      "Ada Lovelace",
		Email:     "ada@example.com",
		BirthDate: timestamppb.New(time.Date(1815, 12, 10, 0, 0, 0, 0, time.UTC)),
		Phones:    []string{"+44 20 7946 0000"},
		Address:   &peoplev1.Address{Street: "St James's Square", City: "London", Country: "UK"},
	})
	if err != nil {
		return err
	}
	fmt.Printf("created %s %s <%s>\n", ada.GetId(), ada.GetName(), ada.GetEmail())
	for _, req := range []*peoplev1.CreatePersonRequest{
		{Name: "Alan Turing", Email: "alan@example.com"},
		{Name: "Grace Hopper", Email: "grace@example.com"},
	} {
		p, err := client.CreatePerson(ctx, req)
		if err != nil {
			return err
		}
		fmt.Printf("created %s %s\n", p.GetId(), p.GetName())
	}

	step("2. unary errors: the server's AppError comes back with grpcx.FromError")
	_, err = client.CreatePerson(ctx, &peoplev1.CreatePersonRequest{Name: "Nobody", Email: "not-an-email"})
	printAppError(err)
	_, err = client.GetPerson(ctx, &peoplev1.GetPersonRequest{Id: "p-404"})
	printAppError(err)

	step("3. server streaming: ListPeople")
	if err := listPeople(ctx, client); err != nil {
		return err
	}

	step("4. client streaming: UploadPhoto in chunks")
	img, err := loadImage(imagePath)
	if err != nil {
		return err
	}
	info, err := uploadPhoto(ctx, client, ada.GetId(), img)
	if err != nil {
		return err
	}
	fmt.Printf("uploaded %d bytes, server says %s, %d bytes, sha256 %.12s…\n",
		len(img), info.GetContentType(), info.GetSize(), info.GetSha256())
	if info.GetSha256() != sha256Hex(img) {
		return errors.New("upload checksum mismatch")
	}

	step("5. server streaming: DownloadPhoto in chunks")
	if err := downloadPhoto(ctx, client, ada.GetId(), out); err != nil {
		return err
	}

	step("6. bidirectional streaming: Chat")
	if err := chat(ctx, client, ada.GetId()); err != nil {
		return err
	}

	fmt.Printf("\nWith http-grpc-server, open http://localhost:8080/people/%s/photo\n", ada.GetId())
	return nil
}

func listPeople(ctx context.Context, client peoplev1.PeopleServiceClient) error {
	stream, err := client.ListPeople(ctx, &peoplev1.ListPeopleRequest{})
	if err != nil {
		return err
	}
	for {
		p, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil // the server finished the stream
		}
		if err != nil {
			return err
		}
		fmt.Printf("  %s  %-14s  %s\n", p.GetId(), p.GetName(), p.GetEmail())
	}
}

func uploadPhoto(ctx context.Context, client peoplev1.PeopleServiceClient, personID string, img []byte) (*peoplev1.PhotoInfo, error) {
	stream, err := client.UploadPhoto(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&peoplev1.UploadPhotoRequest{
		Data: &peoplev1.UploadPhotoRequest_PersonId{PersonId: personID},
	}); err != nil {
		return nil, err
	}
	chunks := 0
	for start := 0; start < len(img); start += uploadChunkSize {
		end := min(start+uploadChunkSize, len(img))
		err := stream.Send(&peoplev1.UploadPhotoRequest{
			Data: &peoplev1.UploadPhotoRequest_Chunk{Chunk: img[start:end]},
		})
		if errors.Is(err, io.EOF) {
			break // the server already answered: CloseAndRecv returns its error
		}
		if err != nil {
			return nil, err
		}
		chunks++
	}
	fmt.Printf("sent %d chunks\n", chunks)
	return stream.CloseAndRecv()
}

func downloadPhoto(ctx context.Context, client peoplev1.PeopleServiceClient, personID, out string) error {
	stream, err := client.DownloadPhoto(ctx, &peoplev1.DownloadPhotoRequest{PersonId: personID})
	if err != nil {
		return err
	}
	var (
		info   *peoplev1.PhotoInfo
		buf    bytes.Buffer
		chunks int
	)
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch data := msg.GetData().(type) {
		case *peoplev1.DownloadPhotoResponse_Info:
			info = data.Info
		case *peoplev1.DownloadPhotoResponse_Chunk:
			buf.Write(data.Chunk)
			chunks++
		}
	}
	if info == nil || sha256Hex(buf.Bytes()) != info.GetSha256() {
		return errors.New("download checksum mismatch")
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		return err
	}
	fmt.Printf("received %d chunks, %d bytes, checksum ok, saved to %s\n", chunks, buf.Len(), out)
	return nil
}

// chat sends and receives at the same time: a goroutine prints whatever the
// server sends while this one sends the messages, then closes its side.
func chat(ctx context.Context, client peoplev1.PeopleServiceClient, personID string) error {
	stream, err := client.Chat(ctx)
	if err != nil {
		return err
	}

	received := make(chan error, 1)
	go func() { received <- printChat(stream) }()

	for _, text := range []string{"hello", "how are you?", "bye"} {
		if err := stream.Send(&peoplev1.ChatMessage{PersonId: personID, Text: text, SentAt: timestamppb.Now()}); err != nil {
			break // the server ended the stream; its status comes from Recv
		}
		fmt.Printf("  → %s\n", text)
	}
	if err := stream.CloseSend(); err != nil {
		return err
	}
	return <-received
}

func printChat(stream grpc.BidiStreamingClient[peoplev1.ChatMessage, peoplev1.ChatMessage]) error {
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Printf("  ← %s: %s\n", msg.GetPersonId(), msg.GetText())
	}
}

// printAppError shows the status the server sent, rebuilt as an AppError.
func printAppError(err error) {
	appErr, ok := grpcx.FromError(err)
	if !ok {
		fmt.Println("unexpected success")
		return
	}
	fmt.Printf("error kind=%s code=%s message=%q\n", appErr.Kind, appErr.Code, appErr.Message)
}

func loadImage(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path) // #nosec G304 -- operator-chosen file
	}
	return generatePNG(), nil
}

// generatePNG draws a 256x256 gradient with noise, which PNG cannot compress
// much, so the transfer takes several chunks.
func generatePNG() []byte {
	const size = 256
	noise := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(noise.IntN(256)), A: 0xff})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func step(title string) { fmt.Printf("\n== %s\n", title) }
