const path = require('node:path');
const CopyPlugin = require('copy-webpack-plugin');

module.exports = {
  entry: { module: './src/module.tsx' },
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: '[name].js',
    library: { type: 'amd' },
    uniqueName: 'mlimarenko-problemdesk-app',
    publicPath: 'public/plugins/mlimarenko-problemdesk-app/',
  },
  externals: ['react', 'react-dom', 'react/jsx-runtime', '@emotion/css', /^@grafana\/(data|runtime|ui)/],
  module: { rules: [{ test: /\.tsx?$/, exclude: /node_modules/, use: { loader: 'ts-loader', options: { transpileOnly: true } } }] },
  resolve: { extensions: ['.tsx', '.ts', '.js'] },
  plugins: [new CopyPlugin({ patterns: [
    { from: 'src/plugin.json', to: 'plugin.json' },
    { from: 'docs/assets/logo.svg', to: 'img/logo.svg' },
    { from: 'docs', to: 'docs' },
    { from: 'README.md', to: 'README.md' },
    { from: 'LICENSE', to: 'LICENSE', toType: 'file' },
    { from: 'CHANGELOG.md', to: 'CHANGELOG.md' },
  ] })],
};
