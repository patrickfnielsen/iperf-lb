# iperf-lb
IPerf3 Loadbalancer that spawns a new IPerf3 server process on-demand.

Based on the IPerf3 cookie we loadbalance sessions to the same server process, this allows an infinite number of tests to run at the same time.

## Credits
Based on https://github.com/inlets/mixctl
