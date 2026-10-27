import { defineConfig } from 'orval'

export default defineConfig({
  api: {
    input: '../api/openapi.yaml',
    output: {
      mode: 'single',
      target: 'src/api/gen/api.ts',
      client: 'react-query',
      httpClient: 'fetch',
      override: {
        mutator: { path: 'src/api/fetcher.ts', name: 'apiFetch' },
        // Hooks return the response body itself, not { data, status, headers }.
        fetch: { includeHttpResponseReturnType: false },
      },
    },
  },
  apiZod: {
    input: '../api/openapi.yaml',
    output: {
      mode: 'single',
      target: 'src/api/gen/api.zod.ts',
      client: 'zod',
    },
  },
})
