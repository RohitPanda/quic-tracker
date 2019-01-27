Add go executable to PATH variable
e.g. export PATH=$PATH:/home/panda/Quic-Test/build/go/bin

export GOPATH=~/go

go get github.com/RohitPanda/quic-tracker  # This will fail because of the missing dependencies that should be build using the 4 lines below
cd $GOPATH/src/github.com/mpiraux/pigotls
make
cd $GOPATH/src/github.com/mpiraux/ls-qpack-go
make

cd $GOPATH/src/github.com/RohitPanda/quic-tracker

go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake.txt -logs-directory . -scenario handshake
cd handshake
files=(*)
for item in ${files[*]}
do
  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
  echo $item,$timestamp>>ietf_handshake.csv
done
cd ..
go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake_v6.txt -logs-directory . -scenario handshake_v6
cd handshake_v6
files=(*)
for item in ${files[*]}
do
  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
  echo $item,$timestamp>>ietf_handshake_v6.csv
done
cd ..
go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake_0rtt.txt -logs-directory .  -scenario zero_rtt
cd zero_rtt
files=(*)
for item in ${files[*]}
do
  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
  echo $item,$timestamp>>ietf_handshake_0rtt.csv
done
cd ..
