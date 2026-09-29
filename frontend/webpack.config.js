const path = require('path');
const fs = require('fs');
const webpack = require('webpack');
const HtmlWebpackPlugin = require('html-webpack-plugin');

// RevenueCat PUBLIC SDK keys, baked into the bundle at build time. Read from the
// environment (Vercel project env) or a local, git-ignored frontend/.env.local.
// Secret keys (sk_...) are server-only and fail the build if supplied here.
function loadRevenueCatKeys() {
  const names = ['REVENUECAT_IOS_API_KEY', 'REVENUECAT_ANDROID_API_KEY', 'REVENUECAT_WEB_API_KEY'];
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
  names.forEach((name) => {
    const value = process.env[name] || local[name] || '';
    if (/^sk_/.test(value)) {
      throw new Error(`${name} looks like a RevenueCat SECRET key — only public SDK keys may be used in the app.`);
    }
    defs[`process.env.${name}`] = JSON.stringify(value);
  });
  return defs;
}

const crmDir = path.resolve(__dirname, 'src/crm');

module.exports = {
  // entry.js lazy-loads either CardFlow (src/index.js, unchanged) or Ajay's CRM (/crm/*),
  // so neither app downloads the other's code.
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
    new webpack.DefinePlugin(loadRevenueCatKeys()),
    new HtmlWebpackPlugin({
      template: path.resolve(__dirname, 'public/index.html'),
      title: 'CardFlow — Business Discovery & Digital Card Vault'
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
    // Same-origin CRM API in dev so its HttpOnly session cookie works (CRM DECISIONS D-05).
    proxy: [
      {
        context: ['/api/crm'],
        target: 'http://127.0.0.1:8080'
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
