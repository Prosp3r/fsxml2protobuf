package main

import (
	"code.google.com/p/goprotobuf/proto"
	"flag"
	"fmt"
	"github.com/DallanQ/fsxml2protobuf/fs_data"
	"io/ioutil"
	"log"
	"os"
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	var numRecords = flag.Int("n", 10, "number of records to dump")
	flag.Parse()
	
	file, err := os.Open(flag.Arg(0))
	check(err)
	defer file.Close()

	bytes, err := ioutil.ReadAll(file)
	check(err)

	fsPersons := &fs_data.FamilySearchPersons{}
	err = proto.Unmarshal(bytes, fsPersons)
	check(err)

	for i := 0; i < *numRecords; i++ {
		fmt.Printf("fsPersons[%d]=%+v\n\n", i, fsPersons.Persons[i])
	}
}
