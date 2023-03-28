package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var rxCount uint64
var txCount uint64

const (
	DEFAULT_WORKER_NUM = 64
	DEFAULT_TARGET_URL = "https://db.laomoe.com/data-waster-dummy"
)

var (
	workerNum = flag.Int("j", DEFAULT_WORKER_NUM, "worker num")
	url       = flag.String("u", DEFAULT_TARGET_URL, "target url")
	verbose   = flag.Bool("v", false, "print more info")
)

func main() {
	// unit is MB
	flag.Usage = func() {
		println("usage: xhll [...opts] [n MB]")
		flag.PrintDefaults()
	}
	flag.Parse()
	stopCount := 10
	// fmt.Println(flag.Args())
	if flag.NArg() > 0 {
		n, err := strconv.Atoi(flag.Arg(0))
		if err != nil {
			flag.Usage()
			return
		}
		stopCount = n
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		c := uint64(stopCount) * 1024 * 1024

		startTime := time.Now()
		txSpeedCalcer := newSpeedCalcer(startTime, 0)
		rxSpeedCalcer := newSpeedCalcer(startTime, 0)
		for {
			rx := atomic.LoadUint64(&rxCount)
			tx := atomic.LoadUint64(&txCount)
			rxtx := rx + tx
			rxSpeed := B2M(rxSpeedCalcer.Calc(int64(rx)))
			txSpeed := B2M(txSpeedCalcer.Calc(int64(tx)))
			eplasedTime := time.Since(startTime)

			cleanLine()
			fmt.Printf("elpased: %s rxtx: %.2fm ↑: %.2fm/s ↓: %.2fm/s", eplasedTime, B2M(float64(rxtx)), txSpeed, rxSpeed)

			if rxtx > c {
				// calc avg speed
				txSpeedCalcer = newSpeedCalcer(startTime, 0)
				rxSpeedCalcer = newSpeedCalcer(startTime, 0)
				rxSpeed = B2M(rxSpeedCalcer.Calc(int64(rx)))
				txSpeed = B2M(txSpeedCalcer.Calc(int64(tx)))

				cleanLine()
				fmt.Printf("elpased: %s rxtx: %.2fm ↑: %.2fm/s ↓: %.2fm/s", eplasedTime, B2M(float64(rxtx)), txSpeed, rxSpeed)
				println("")
				println("done!")
				cancel()
				return
			}

			time.Sleep(time.Millisecond * 500)

		}
	}()

	ch := make(chan func(), *workerNum)
	for i := 0; i < *workerNum; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case task := <-ch:
					task()
				}
			}
		}()
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				ch <- func() {
					err := download(ctx, *url)
					if err != nil {

						if *verbose {
							println(err.Error())
						}
						time.Sleep(time.Second)

					}
				}
			}
		}
	}()

	<-ctx.Done()

}

var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 1024)
		return &buf
	},
}

func download(ctx context.Context, url string) error {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	client := http.Client{Transport: tr}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Close = true
	req.Header.Set("User-Agent", DEFAULT_UA)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	type writer interface {
		Write(w io.Writer) error
	}
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)
	count := func(addr *uint64, w writer) {
		pipr, pipw := io.Pipe()
		go func() {
			w.Write(pipw)
			pipw.Close()
		}()
		for {
			n, err := pipr.Read(*buf)
			if err != nil {
				pipr.Close()
				return
			}
			atomic.AddUint64(addr, uint64(n))
		}
	}

	count(&txCount, req)
	count(&rxCount, resp)

	return nil
}

func cleanLine() {
	fmt.Printf("\033[2K\r")
}

type SpeedCalcer struct {
	preTime  time.Time
	preValue int64
}

func newSpeedCalcer(t time.Time, v int64) *SpeedCalcer {
	return &SpeedCalcer{preTime: t, preValue: v}
}

// no support concurrent
func (c *SpeedCalcer) Calc(v int64) float64 {
	delta := v - c.preValue
	since := time.Since(c.preTime)
	r := float64(delta) / since.Seconds()
	c.preTime = time.Now()
	c.preValue = v
	return r

}

// byte to mb
func B2M(v float64) float64 {
	return v / 1024 / 1024
}

const DEFAULT_UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.0.0 Safari/537.36Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.0.0 Safari/537.36"
