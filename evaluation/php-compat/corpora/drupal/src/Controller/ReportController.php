<?php

declare(strict_types=1);

namespace Drupal\report\Controller;

use Drupal\Core\Controller\ControllerBase;
use Drupal\report\Service\ReportRepository;

final class ReportController extends ControllerBase
{
    public function __construct(private ReportRepository $reports)
    {
    }

    public function summary(): array
    {
        return [
            '#theme' => 'report_summary',
            '#reports' => $this->reports->recent(),
        ];
    }
}
