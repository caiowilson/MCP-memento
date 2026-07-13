<?php

declare(strict_types=1);

namespace Drupal\report\Plugin\Block;

use Drupal\Core\Block\Attribute\Block;
use Drupal\Core\Block\BlockBase;
use Drupal\Core\StringTranslation\TranslatableMarkup;
use Drupal\report\Service\ReportRepository;

#[Block(
    id: 'report_summary',
    admin_label: new TranslatableMarkup('Report summary'),
)]
final class ReportBlock extends BlockBase
{
    public function __construct(private ReportRepository $reports)
    {
    }

    public function build(): array
    {
        return [
            '#theme' => 'report_summary',
            '#reports' => $this->reports->recent(),
        ];
    }
}
