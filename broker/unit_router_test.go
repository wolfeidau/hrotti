package hrotti

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_AddSub(t *testing.T) {
	rootNode := NewNode("")
	rand.Seed(time.Now().UnixNano())
	topics := [7]string{"a", "b", "c", "d", "e", "+", "#"}
	for i := 0; i < 20; i++ {
		c := newClient(nil, "testClientId"+strconv.Itoa(i), &User{}, 100)
		var sub string
		r := rand.Intn(7)
		for j := 0; j <= r; j++ {
			char := topics[rand.Intn(7)]
			sub += char
			if char == "#" || j == r {
				break
			}
			sub += "/"
		}
		rootNode.AddSub(c, strings.Split(sub, "/"), 1)
	}
}

func BenchmarkNormalRouter(b *testing.B) {
	rootNode := NewNode("")
	rand.Seed(time.Now().UnixNano())
	topics := [7]string{"a", "b", "c", "d", "e", "+", "#"}
	for i := 0; i < b.N; i++ {
		c := newClient(nil, "testClientId"+strconv.Itoa(i), 100)
		var sub string
		r := rand.Intn(7)
		for j := 0; j <= r; j++ {
			char := topics[rand.Intn(7)]
			sub += char
			if char == "#" || j == r {
				break
			}
			sub += "/"
		}
		rootNode.AddSub(c, strings.Split(sub, "/"), 1)
	}
	var treeWorkers sync.WaitGroup
	recipients := make(chan *Entry)
	b.ResetTimer()
	treeWorkers.Add(1)
	rootNode.FindRecipients(strings.Split("a/b/c/d/e", "/"), recipients, &treeWorkers)
	treeWorkers.Wait()
	close(recipients)
	for {
		_, ok := <-recipients
		if !ok {
			break
		}
	}
}

func main() {
	br := testing.Benchmark(BenchmarkNormalRouter)
	fmt.Println(br)
}
