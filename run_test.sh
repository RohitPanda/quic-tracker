#!/bin/bash
for ((i=1; i<=20; i++ ))
do
	go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake.txt -logs-directory . -scenario handshake
	cd handshake
	files=(*)
	for item in ${files[*]}
	do
	  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
	  echo $item,$timestamp>>../ietf_handshake.csv
	done
	cd ..
	go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake_v6.txt -logs-directory . -scenario handshake_v6
	cd handshake_v6
	files=(*)
	for item in ${files[*]}
	do
	  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
	  echo $item,$timestamp>>../ietf_handshake_v6.csv
	done
	cd ..
	go run bin/test_suite/test_suite.go -hosts ietf_quic_hosts_handshake_0rtt.txt -logs-directory .  -scenario zero_rtt
	cd zero_rtt
	files=(*)
	for item in ${files[*]}
	do
	  timestamp=$(cat $item | grep -h -o -P '(?<=timestamp=)(\d+)' | tr '\n' ',' | sed 's/.$//')
	  echo $item,$timestamp>>../ietf_handshake_0rtt.csv
	done
	cd ..
	sleep 5s
done
