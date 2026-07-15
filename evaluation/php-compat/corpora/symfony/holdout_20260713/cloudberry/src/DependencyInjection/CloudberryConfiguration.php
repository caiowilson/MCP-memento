<?php
declare(strict_types=1);

namespace App\DependencyInjection\Cloudberry;

use Symfony\Component\Config\Definition\Builder\TreeBuilder;
use Symfony\Component\Config\Definition\ConfigurationInterface;

final class CloudberryConfiguration implements ConfigurationInterface
{
    public function getConfigTreeBuilder(): TreeBuilder
    {
        $treeBuilder = new TreeBuilder('cloudberry');

        $treeBuilder->getRootNode()
            ->children()
                ->integerNode('spool_batch_size')
                    ->min(1)
                    ->defaultValue(48)
                ->end()
            ->end();

        return $treeBuilder;
    }
}
