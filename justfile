set shell := ["pwsh", "-NoProfile", "-Command"]

artifacts := env_var_or_default("ARTIFACTS", "artifacts")

configuration := env_var_or_default("CONFIGURATION", "Release")

# Default
all: check pack

# Run all tests
test:
    dotnet test DevLocal.slnx -c {{configuration}}

# Run formatting check
format:
    dotnet format DevLocal.slnx --verify-no-changes

# Run formatting check and tests
check: format test

# Pack the tool package into artifacts/
pack:
    dotnet pack src/DevLocal.Tool/DevLocal.Tool.csproj -c {{configuration}} -o {{artifacts}}

# Publish a self-contained runnable tool into artifacts/publish/
publish:
    dotnet publish src/DevLocal.Tool/DevLocal.Tool.csproj -c {{configuration}} -o {{artifacts}}/publish

# Build the solution
build:
    dotnet build DevLocal.slnx -c {{configuration}}

# Clean build output
clean:
    dotnet clean DevLocal.slnx -c {{configuration}}