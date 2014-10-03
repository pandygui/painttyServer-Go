package Radio

import (
	"BufferedFile"
	"Socket"
	"encoding/hex"
	xxhash "github.com/OneOfOne/xxhash/native"
	"github.com/dustin/randbo"
	"io"
	"strconv"
	"strings"
)

import "log"

const (
	CHUNK_SIZE          int64 = 1024 * 400 // Bytes
	MAX_CHUNKS_IN_QUEUE       = 2048       // which means there shuold be 2048 RadioChunk instances in pending queue at most
	SEND_INTERVAL             = 500        // check pending list every 800ms to send new items
)

// TODO: all need test
// TODO: use type switch

func splitChunk(chunk FileChunk) []RadioChunk {
	var result_queue = make([]RadioChunk, 0)
	var real_chunk_size = CHUNK_SIZE
	var chunks = chunk.Length / real_chunk_size

	// keep chunks in a reasonable amount
	for chunks > MAX_CHUNKS_IN_QUEUE {
		real_chunk_size *= 2
		chunks = chunk.Length / real_chunk_size
	}
	var c_pos = chunk.Start
	for i := 0; int64(i) < chunks; i++ {
		result_queue = append(result_queue, FileChunk{c_pos, real_chunk_size})
		c_pos += real_chunk_size
	}

	if chunk.Length%real_chunk_size > 0 {
		result_queue = append(result_queue, FileChunk{c_pos, chunk.Length % real_chunk_size})
	}

	return result_queue
}

func pushLargeChunk(chunk FileChunk, queue *RadioTaskList) {
	var new_items = splitChunk(chunk)
	queue.Append(new_items)
}

func pushRamChunk(chunk RAMChunk, queue *RadioTaskList) {
	// re-split chunk in ram won't save any memory, so just make it in queue
	queue.Append([]RadioChunk{chunk})
}

func appendToPendings(chunk RadioChunk, list *RadioTaskList) {
	log.Println("appended", chunk)
	switch chunk.(type) {
	case RAMChunk:
		pushRamChunk(chunk.(RAMChunk), list)
		return
	}
	var chunkF = chunk.(FileChunk)

	if list.Length() > 0 {
		var bottomItem = list.PopBack()
		switch bottomItem.(type) {
		case FileChunk:
			var bottomItemF = bottomItem.(FileChunk)
			// try to merge new chunk into old chunk
			var new_length = bottomItemF.Length + chunkF.Length
			if bottomItemF.Start+bottomItemF.Length == chunkF.Start { // if two chunks are neighbor
				// concat two chunks and re-split them
				pushLargeChunk(FileChunk{bottomItemF.Start, new_length}, list)
			} else { // or just push those in
				list.Append([]RadioChunk{bottomItemF})                       // push the old chunk back
				pushLargeChunk(FileChunk{chunkF.Start, chunkF.Length}, list) // and new one
			}
		case RAMChunk:
			// special RadioRAMChunk should be considered
			// TODO: merge RadioRAMChunk if possible
			list.Append([]RadioChunk{bottomItem}) // put it back, since we don't merge anything now
			pushLargeChunk(chunkF, list)
		}
	} else {
		pushLargeChunk(FileChunk{chunkF.Start, chunkF.Length}, list)
	}

	if list.Length() >= MAX_CHUNKS_IN_QUEUE*2 {
		// TODO: add another function to re-split chunks in queue
		//logger.warn('There\'re ', list.length, 'chunks in a single queue!')
	}
}

func fetchAndSend(client *Socket.SocketClient, list *RadioTaskList, file *BufferedFile.BufferedFile) {
	//log.Println("fetchAndSend", list.Tasks())
	//log.Println("tasks fetchAndSend", list.tasks, len(tasks))
	if list.Length() <= 0 {
		return
	}

	var item = list.PopFront()

	switch item.(type) {
	case FileChunk:
		var item = item.(FileChunk)
		var buf = make([]byte, item.Length)
		length, err := file.ReadAt(buf, item.Start)
		//log.Println("fetched length", length)
		if int64(length) != item.Length || err != nil {
			// move back
			list.PushFront(item)
			return
		}
		log.Println("write to client", len(buf))
		go client.WriteRaw(buf)
	case RAMChunk:
		log.Println("write ram chunk to client", len(item.(RAMChunk).Data))
		go client.WriteRaw(item.(RAMChunk).Data)
	}
}

func genArchiveSign(name string) string {
	h := xxhash.New64()
	var buf = make([]byte, 16)
	randbo.New().Read(buf)
	r := strings.NewReader(name + hex.EncodeToString(buf))
	io.Copy(h, r)
	hash := h.Sum64()
	return strconv.FormatUint(hash, 32)
}
