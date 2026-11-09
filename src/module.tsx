import { AppPlugin } from '@grafana/data';
import { Config } from './pages/Config';
import { Problems } from './pages/Problems';
import { Options } from './model';

export const plugin = new AppPlugin<Options>().setRootPage(Problems).addConfigPage({
  id: 'configuration', title: 'Configuration', icon: 'cog', body: Config,
});
