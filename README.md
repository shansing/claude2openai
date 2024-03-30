# Claude2OpenAI
Used to convert the Claude API to OpenAI compatible API. **Easily use Claude with any OpenAI compatible client.**

## Compatibility
Currently it is only compatible with the Claude-3 family of models, if you pass in any other model, the default will be to use **claude-3-haiku-20240307**.

## Usage
### Docker

```bash
docker run -d --restart always -p 6600:6600 ghcr.io/missuo/claude2openai:latest
```

```bash
docker run -d --restart always -p 6600:6600 missuo/claude2openai:latest
```

### Docker Compose
It is recommended that you use docker version **26.0.0** or higher, otherwise you need to specify the version in the `compose.yaml` file.
```diff
+version: "3.9"
```

```bash
mkdir claude2openai && cd claude2openai
wget -O compose.yaml https://raw.githubusercontent.com/missuo/claude2openai/main/compose.yaml
docker compose up -d
```

### Manual

Download the latest release from the [release page](https://github.com/missuo/claude2openai/releases).

```bash
chmod +x claude2openai
./claude2openai
```