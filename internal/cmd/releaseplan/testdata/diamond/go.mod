module example.com/root

go 1.26.0

require (
	example.com/root/moda v0.0.0
	example.com/root/modb v0.0.0
)

replace (
	example.com/root/moda => ./moda
	example.com/root/modb => ./modb
)
