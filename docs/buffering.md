# buffering

The relay can be configured to buffer failed requests for HTTP backends. The
intent of this logic is reduce the number of failures during short outages or
periodic network issues.

> This retry logic is **NOT** sufficient for long periods of downtime as all
> data is buffered in RAM

Buffering has the following configuration options (configured per HTTP backend):

* buffer-size-mb -- An upper limit on how much point data to keep in memory (in
 MB)
* max-batch-kb -- A maximum size on the aggregated batches that will be
 submitted (in KB)
* max-delay-interval -- The max delay between retry attempts per backend. The
 initial retry delay is 500ms and is doubled after every failure.

If the buffer is full then requests are dropped and an error is logged. If a
requests makes it into the buffer it is retried until success.

Retries are serialized to a single backend. In addition, writes will be
aggregated and batched as long as the body of the request will be less than
`max-batch-kb`.If buffered requests succeed then there is no delay between
subsequent attempts.

One can force the retry buffer(s) to be flushed by querying the `/admin/flush`
route. Any data stored in the buffer(s) will be lost.

If the relay stays alive the entire duration of a downed backend server without
filling that server's allocated buffer, and the relay can stay online until the
entire buffer is flushed, it would mean that no operator intervention would be
required to "recover" the data. The data will simply be batched together and
written out to the recovered server in the order it was received.

*NOTE*: The limits for buffering are not hard limits on the memory usage of the
application, and there will be additional overhead that would be much more
challenging to account for. The limits listed are just for the amount of point
line protocol (including any added timestamps, if applicable). Factors such as
small incoming batch sizes and a smaller max batch size will increase the
overhead in the buffer. There is also the general application memory overhead
to account for. This means that a machine with 2GB of memory should not have
buffers that sum up to _almost_ 2GB. The buffering feature will only be
activated when at least two InfluxDB backends are configured. In addition
always at least one backend has to be active for buffering to work.
