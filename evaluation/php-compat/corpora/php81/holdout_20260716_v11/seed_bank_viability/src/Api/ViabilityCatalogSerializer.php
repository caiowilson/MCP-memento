<?php

declare(strict_types=1);

namespace SeedArchive\Api;

use SeedArchive\Domain\SeedLotViability;

final class ViabilityCatalogSerializer
{
    /**
     * @return array{category: string, catalog_spelling: string}
     */
    public function serialize(SeedLotViability $category): array
    {
        return [
            'category' => strtolower($category->name),
            'catalog_spelling' => $category->value,
        ];
    }
}
