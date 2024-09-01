package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nfnt/resize"
	"github.com/patrickmn/go-cache"
)

const (
	UPLOAD_FOLDER    = "./uploads"
	MAX_UPLOAD_SIZE  = 4 << 20
	CONCURRENT_LIMIT = 10 // Limit concurrent image processing
)

var (
	imgCache *cache.Cache
	wg       sync.WaitGroup
	sem      = make(chan struct{}, CONCURRENT_LIMIT) // Semaphore to limit concurrent goroutines
)

type UploadResult struct {
	FileName string `json:"file_name"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

func ensureUploadFolderExists() {
	if _, err := os.Stat(UPLOAD_FOLDER); os.IsNotExist(err) {
		os.MkdirAll(UPLOAD_FOLDER, os.ModePerm) // Create the directory if it does not exist
	}
}

func init() {
	imgCache = cache.New(5*time.Minute, 10*time.Minute)
}

func generateUniqueFileName(originalName string) string {
	ext := filepath.Ext(originalName)
	name := strings.TrimSuffix(originalName, ext)
	return fmt.Sprintf("%s_%s%s", name, uuid.New().String(), ext)
}

func processAndSaveImage(handler *multipart.FileHeader, file io.Reader, ext string, ch chan<- UploadResult) {
	defer wg.Done()
	defer func() { <-sem }() // Release semaphore slot

	img, _, err := image.Decode(file)
	if err != nil {
		ch <- UploadResult{FileName: handler.Filename, Status: "failed", Error: "Error decoding image"}
		return
	}

	if img.Bounds().Dx() != 1920 || img.Bounds().Dy() != 1080 {
		img = resize.Resize(1920, 1080, img, resize.Lanczos3)
	}

	cacheKey := handler.Filename
	if cachedImg, found := imgCache.Get(cacheKey); found {
		img = cachedImg.(image.Image)
	} else {
		imgCache.Set(cacheKey, img, cache.DefaultExpiration)
	}

	uniqueFileName := generateUniqueFileName(handler.Filename)
	filePath := filepath.Join(UPLOAD_FOLDER, uniqueFileName)
	outFile, err := os.Create(filePath)
	if err != nil {
		ch <- UploadResult{FileName: handler.Filename, Status: "failed", Error: "Could not create file"}
		return
	}
	defer outFile.Close()

	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(outFile, img, nil)
	case ".png":
		err = png.Encode(outFile, img)
	}
	if err != nil {
		ch <- UploadResult{FileName: handler.Filename, Status: "failed", Error: "Could not save resized image"}
		return
	}

	ch <- UploadResult{FileName: uniqueFileName, Status: "success"}
}

func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MAX_UPLOAD_SIZE)
	err := r.ParseMultipartForm(MAX_UPLOAD_SIZE)
	if err != nil {
		http.Error(w, "File too large. File should be under 4 MB", http.StatusRequestEntityTooLarge)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	results := make([]UploadResult, 0, len(files))
	ch := make(chan UploadResult, len(files))

	for _, handler := range files {
		file, err := handler.Open()
		if err != nil {
			results = append(results, UploadResult{FileName: handler.Filename, Status: "failed", Error: "Could not open uploaded file"})
			continue
		}

		if handler.Size > MAX_UPLOAD_SIZE {
			results = append(results, UploadResult{FileName: handler.Filename, Status: "failed", Error: "File size exceeds 4 MB limit"})
			continue
		}

		ext := strings.ToLower(filepath.Ext(handler.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			results = append(results, UploadResult{FileName: handler.Filename, Status: "failed", Error: "Only .jpg, .jpeg, and .png files are allowed"})
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot
		go processAndSaveImage(handler, file, ext, ch)
	}

	wg.Wait()
	close(ch)
	for result := range ch {
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func main() {
	ensureUploadFolderExists()

	http.HandleFunc("/upload", uploadFileHandler)

	log.Println("Starting server on :8090...")
	err := http.ListenAndServe(":8090", nil)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
