package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"

	"github.com/Linux-DEX/azstorecli/internal/config"
)

func TestReproCreateThenList(t *testing.T) {
	p := config.AzuriteProfile("local", 10000, 10001, 10002)
	const official = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	p.AccountKey = official
	p.ConnectionString = "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=" + official + ";BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;QueueEndpoint=http://127.0.0.1:10001/devstoreaccount1;TableEndpoint=http://127.0.0.1:10002/devstoreaccount1;"
	c := New(p)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	name := fmt.Sprintf("repro%d", time.Now().Unix()%100000)
	if err := c.CreateContainer(ctx, name, ""); err != nil {
		t.Fatal("create container:", err)
	}
	if err := c.CreateQueue(ctx, name); err != nil {
		t.Fatal("create queue:", err)
	}

	containers, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatal("list containers:", err)
	}
	var sawC bool
	for _, item := range containers {
		if item.Name == name {
			sawC = true
		}
	}
	t.Logf("containers (%d) contains %s: %v", len(containers), name, sawC)
	for _, item := range containers {
		t.Logf("  container %q access=%q", item.Name, item.Access)
	}

	queues, err := c.ListQueues(ctx)
	if err != nil {
		t.Fatal("list queues:", err)
	}
	var sawQ bool
	for _, item := range queues {
		if item.Name == name {
			sawQ = true
		}
	}
	t.Logf("queues (%d) contains %s: %v", len(queues), name, sawQ)
	for _, item := range queues {
		t.Logf("  queue %q msgs=%d meta=%v", item.Name, item.ApproxMessages, item.Metadata)
	}

	blob, err := c.Blob()
	if err != nil {
		t.Fatal(err)
	}
	pager := blob.NewListContainersPager(&azblob.ListContainersOptions{
		Include: azblob.ListContainersInclude{Metadata: true},
	})
	var metaNames []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			t.Fatal("list containers metadata:", err)
		}
		for _, item := range page.ContainerItems {
			if item != nil && item.Name != nil {
				metaNames = append(metaNames, *item.Name)
			}
		}
	}
	t.Logf("containers with include=metadata: %v", metaNames)

	q, err := c.Queue()
	if err != nil {
		t.Fatal(err)
	}
	qp := q.NewListQueuesPager(nil)
	var plain []string
	for qp.More() {
		page, err := qp.NextPage(ctx)
		if err != nil {
			t.Fatal("list queues no metadata:", err)
		}
		for _, item := range page.Queues {
			if item != nil && item.Name != nil {
				plain = append(plain, *item.Name)
			}
		}
	}
	t.Logf("queues without include=metadata: %v", plain)

	qp2 := q.NewListQueuesPager(&azqueue.ListQueuesOptions{
		Include: azqueue.ListQueuesInclude{Metadata: true},
	})
	var withMeta []string
	for qp2.More() {
		page, err := qp2.NextPage(ctx)
		if err != nil {
			t.Fatal("list queues metadata:", err)
		}
		for _, item := range page.Queues {
			if item != nil && item.Name != nil {
				withMeta = append(withMeta, *item.Name)
			} else {
				t.Logf("queue item missing name: %#v", item)
			}
		}
	}
	t.Logf("queues with include=metadata: %v", withMeta)
}
