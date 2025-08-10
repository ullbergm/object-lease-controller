const { ConsoleRemotePlugin } = require('@openshift-console/dynamic-plugin-sdk-webpack');
const CopyWebpackPlugin = require('copy-webpack-plugin');
const path = require('path');

/** @type {import('webpack').Configuration} */
module.exports = {
  context: path.resolve(__dirname, 'src'),
  entry: {},
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: '[name].js',
    publicPath: '/',
    clean: true,
  },
  resolve: {
    extensions: ['.ts', '.tsx', '.js'],
  },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        use: 'ts-loader',
        exclude: /node_modules/,
      },
      {
        test: /\.css$/,
        use: ['style-loader', 'css-loader'],
      },
      {
        test: /\.(png|jpe?g|gif|svg|ttf|eot|woff2?)$/i,
        type: 'asset/resource',
      },
    ],
  },
  plugins: [
    new ConsoleRemotePlugin({
      pluginMetadata: {
        name: 'object-lease-console-plugin',
        version: '0.1.0',
        displayName: 'Object Lease Console Plugin',
        description: 'Navigation and pages to view Kubernetes Lease resources',
        exposedModules: {
          LeasesPage: './components/LeasesPage',
        },
        dependencies: {
          '@console/pluginAPI': '*',
        },
      },
      customProperties : {
        "console": {
            "displayName": "Object Lease Plugin",
            "description": "A plugin to manage Kubernetes Lease resources"
        }
      },
      extensions: [
        {
        "type": "console.navigation/href",
        "properties": {
            "id": "object-lease-operator",
            "name": "Object Leases",
            "href": "/object-lease/leases",
            "perspective": "admin"
        }
        },
        {
          type: 'console.page/route',
          properties: {
            path: ['/object-lease/leases'],
            exact: true,
            component: { $codeRef: 'LeasesPage' },
          },
        },
      ],
    }),
    new CopyWebpackPlugin({
      patterns: [{ from: path.resolve(__dirname, 'public'), to: path.resolve(__dirname, 'dist') }],
    }),
  ],
  devServer: {
    static: false,
    port: 9001,
    compress: true,
    historyApiFallback: true,
    allowedHosts: 'all',
    headers: {
      'Access-Control-Allow-Origin': '*',
    },
  },
};
