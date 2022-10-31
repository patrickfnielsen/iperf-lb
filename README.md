# iperf-lb
IPerf3 Loadbalancer that spawns a new IPerf3 server process on-demand.

Allowing for multiple clients to run tests on the same time.


## Limits
While this allows multiple clients, they need to come from different source IP's as it's used for sticky sessions. 

The reason for this is that IPerf3 creates two TCP sessions for each test, and they need to hit the same IPerf3 proccess.


## Credits
Based on https://github.com/inlets/mixctl
