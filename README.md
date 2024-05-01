# iperf-lb
IPerf3 Loadbalancer that spawns a new IPerf3 server process on-demand.

Based on the IPerf3 cookie we loadbalance sessions to the same server process, this allows an infinite number of tests to run at the same time.

## Setup
Download the latest release and run it.

The following arguments are supported:
| Argument    | Description                                | Default          |
| ----------- | ------------------------------------------ | ---------------- |
| l           | The ip and port to listen to               | 5201             |
| t           | Dial timeout to the iperf server           | 1500 millisecond |
| metrics     | Set to enable prometheus metrics on :2112  | false            |


## Credits
Based on https://github.com/inlets/mixctl
