const path = require('path');
const fs = require('fs');
const webpack = require('webpack');
const HtmlWebpackPlugin = require('html-webpack-plugin');

// Build-time settings baked into the bundle. Read from the environment (Vercel project
// env) or a local, git-ignored frontend/.env.local.
function loadBuildEnv() {
  const local = {};
  try {
    fs.readFileSync(path.resolve(__dirname, '.env.local'), 'utf8')
      .split('\n')
      .forEach((line) => {
        const m = /^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/.exec(line);
        if (m) local[m[1]] = m[2].replace(/^['"]|['"]$/g, '');
      });
  } catch (e) {}
  const defs = {};
  // Where the native app shell finds the API. Set CRM_API_ORIGIN when the server moves;
  // the default is today's server so an unset build keeps working.
  const origin = process.env.CRM_API_ORIGIN || local.CRM_API_ORIGIN || 'https://cardflow-api-fsij.onrender.com';
  defs['process.env.CRM_API_ORIGIN'] = JSON.stringify(origin.replace(/\/+$/, ''));
  return defs;
}

const crmDir = path.resolve(__dirname, 'src/crm');

module.exports = {
  // One app for every URL; the phone layout is a lazy chunk (src/MobileRoot.js).
  entry: path.resolve(__dirname, 'src/entry.js'),
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: 'bundle.[contenthash].js',
    publicPath: '/',
    clean: true
  },
  resolve: {
    alias: {
      'react-native$': 'react-native-web',
      '@crm': crmDir
    },
    extensions: ['.web.js', '.js', '.json', '.ts', '.tsx']
  },
  module: {
    rules: [
      {
        test: /\.(js|jsx|ts|tsx)$/,
        exclude: /node_modules[\\/](?!(react-native-web|lucide-react)[\\/])/,
        use: {
          loader: 'babel-loader',
          options: {
            configFile: path.resolve(__dirname, 'babel.config.js')
          }
        }
      },
      {
        test: /\.css$/,
        exclude: crmDir,
        use: ['style-loader', 'css-loader']
      },
      {
        // Tailwind for the CRM only; config lives in src/crm (postcss.config.js).
        test: /\.css$/,
        include: crmDir,
        use: ['style-loader', 'css-loader', 'postcss-loader']
      },
      {
        test: /\.(png|jpe?g|gif|svg)$/i,
        type: 'asset/resource'
      }
    ]
  },
  plugins: [
    new webpack.DefinePlugin(loadBuildEnv()),
    new HtmlWebpackPlugin({
      template: path.resolve(__dirname, 'public/index.html'),
      title: "Ajay's CRM"
    })
  ],
  watchOptions: {
    ignored: /node_modules/,
    poll: 1000
  },
  devServer: {
    host: '127.0.0.1',
    port: 3000,
    historyApiFallback: true,
    hot: true,
    // Same-origin API in dev so the HttpOnly session cookie works for the CRM API and
    // the app API alike (CRM DECISIONS D-05, D-104).
    proxy: [
      {
        context: ['/api'],
        // The local API for the dev server; API_PROXY_TARGET overrides it.
        target: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8080'
      }
    ],
    open: false,
    static: {
      directory: path.resolve(__dirname, 'public'),
      watch: false
    },
    client: {
      logging: 'warn',
      overlay: {
        errors: true,
        warnings: false
      }
    }
  }
};
