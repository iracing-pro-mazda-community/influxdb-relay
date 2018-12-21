# filters

You can use regular expression in order to filter in tags and measurements.

Here is an example configuration snippet:

```toml
[[filter]]
tag-expression = "^.{0,5}$"
measurement-expression = "^.{0,8}$"
outputs = [ "from_influx_1", "from_influx_3" ]

[[filter]]
tag-expression = "^.{5,12}$"
outputs = [ "from_influx_2" ]
```

Here, I'm creating two filters.

The first filter will apply on both tags and measurements for any incoming
request for the endpoints `from_influx_1` and `from_influx_3`. The tag length
must be between 0 and 5 included whereas the measurement length must be
between 0 and 8, also included. If it is not the case, the relay does not
forward the query.

The second filter will apply on tags for `from_influx_2` and will check if
their length is between five and twelve.
