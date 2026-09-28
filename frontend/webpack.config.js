const path = require('path');
const HtmlWebpackPlugin = require('html-webpack-plugin');

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
